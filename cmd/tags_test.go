package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/dockerhub"
)

// fetchTagsFromMock serves one page of tags JSON and returns the normalized
// Tags. FetchTags is the only way from outside package dockerhub to build
// Tags whose unexported archSet is populated (needed by HasArch-based paths).
func fetchTagsFromMock(t *testing.T, body string, showOS bool) []dockerhub.Tag {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	tags, err := dockerhub.FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, showOS)
	if err != nil {
		t.Fatalf("FetchTags from mock: %v", err)
	}
	return tags
}

// mockTagsBody: three tags — amd64+arm64, amd64-only, 386-only.
const mockTagsBody = `{
 "count": 3, "next": "", "results": [
  {"name":"multi","tag_status":"active","full_size":104857600,"last_updated":"2026-03-03T00:00:00Z",
   "images":[{"architecture":"amd64","os":"linux"},{"architecture":"arm64","variant":"v8","os":"linux"}]},
  {"name":"amd-only","tag_status":"active","full_size":52428800,"last_updated":"2026-02-02T00:00:00Z",
   "images":[{"architecture":"amd64","os":"linux"}]},
  {"name":"x86-only","tag_status":"inactive","full_size":20971520,"last_updated":"2026-01-01T00:00:00Z",
   "images":[{"architecture":"386","os":"linux"}]}
 ]}`

func TestParseImageRef(t *testing.T) {
	cases := []struct {
		in       string
		wantUser string
		wantName string
	}{
		{"nginx", "library", "nginx"},                       // official default
		{"bitnami/nginx", "bitnami", "nginx"},               // user/image
		{"registry.io/team/app", "registry.io", "team/app"}, // first slash splits
		{"user/", "user", ""},                               // trailing slash: empty name
		{"/img", "", "img"},                                 // leading slash: empty user
		{"", "library", ""},                                 // empty ref
	}
	for _, c := range cases {
		u, n := parseImageRef(c.in)
		if u != c.wantUser || n != c.wantName {
			t.Errorf("parseImageRef(%q) = (%q, %q), want (%q, %q)", c.in, u, n, c.wantUser, c.wantName)
		}
	}
}

func TestParseArchFlag(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"amd64", []string{"amd64"}},
		{"amd64,arm64", []string{"amd64", "arm64"}},
		{" amd64 , arm64 ", []string{"amd64", "arm64"}}, // surrounding spaces trimmed
		{"amd64,,arm64", []string{"amd64", "arm64"}},    // empty parts dropped
		{" , , ", []string{}},                           // only empties: empty (len 0)
	}
	for _, c := range cases {
		got := parseArchFlag(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseArchFlag(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseArchFlag(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestFilterByName(t *testing.T) {
	tags := []dockerhub.Tag{
		{Name: "1.27-alpine"}, {Name: "1.27"}, {Name: "latest"},
	}
	if got := filterByName(tags, "1.27"); len(got) != 2 {
		t.Errorf("filter 1.27 = %d tags, want 2", len(got))
	}
	if got := filterByName(tags, "1.27-alpine"); len(got) != 1 || got[0].Name != "1.27-alpine" {
		t.Errorf("exact name filter = %+v", got)
	}
	if got := filterByName(tags, "ALPINE"); len(got) != 0 {
		t.Errorf("filter is case-sensitive: got %d, want 0", len(got))
	}
	if got := filterByName(tags, "no-match"); len(got) != 0 {
		t.Errorf("no match = %d, want 0", len(got))
	}
	if got := filterByName(nil, "x"); len(got) != 0 {
		t.Errorf("nil input = %d, want 0", len(got))
	}
}

func TestFilterByArch(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)

	amd := filterByArch(tags, []string{"amd64"})
	if len(amd) != 2 || amd[0].Name != "multi" || amd[1].Name != "amd-only" {
		t.Errorf("amd64 filter = %v", names(amd))
	}
	arm := filterByArch(tags, []string{"arm64"})
	if len(arm) != 1 || arm[0].Name != "multi" {
		t.Errorf("arm64 filter = %v", names(arm))
	}
	// Multi-target OR semantics: amd64 OR arm64 OR 386 => all three tags.
	any := filterByArch(tags, []string{"amd64", "arm64", "386"})
	if len(any) != 3 {
		t.Errorf("multi-target filter = %v, want all 3", names(any))
	}
	if got := filterByArch(tags, []string{"riscv64"}); len(got) != 0 {
		t.Errorf("unknown arch = %v, want none", names(got))
	}
	if got := filterByArch(tags, nil); len(got) != 0 {
		t.Errorf("no targets = %v, want none", names(got))
	}
}

func names(tags []dockerhub.Tag) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

func TestSortTags(t *testing.T) {
	tags := []dockerhub.Tag{
		{Name: "b", SizeMB: 20, LastUpdated: "2026-02-01T00:00:00Z"},
		{Name: "a", SizeMB: 30, LastUpdated: "2026-01-01T00:00:00Z"},
		{Name: "c", SizeMB: 10, LastUpdated: "2026-03-01T00:00:00Z"},
	}

	byName := append([]dockerhub.Tag(nil), tags...)
	sortTags(byName, "name", true)
	if byName[0].Name != "a" || byName[2].Name != "c" {
		t.Errorf("name asc = %v", names(byName))
	}
	sortTags(byName, "name", false)
	if byName[0].Name != "c" || byName[2].Name != "a" {
		t.Errorf("name desc = %v", names(byName))
	}

	bySize := append([]dockerhub.Tag(nil), tags...)
	sortTags(bySize, "size", true)
	if bySize[0].Name != "c" || bySize[2].Name != "a" {
		t.Errorf("size asc = %v (c=10 < b=20 < a=30)", names(bySize))
	}

	byDate := append([]dockerhub.Tag(nil), tags...)
	sortTags(byDate, "date", false) // default sort: newest first
	if byDate[0].Name != "c" || byDate[2].Name != "a" {
		t.Errorf("date desc = %v", names(byDate))
	}
	sortTags(byDate, "unknown-key", true) // falls back to date
	if byDate[0].Name != "a" || byDate[2].Name != "c" {
		t.Errorf("unknown key falls back to date asc = %v", names(byDate))
	}
}

func TestSortTags_StableForEqualKeys(t *testing.T) {
	tags := []dockerhub.Tag{
		{Name: "first", LastUpdated: "2026-01-01T00:00:00Z"},
		{Name: "second", LastUpdated: "2026-01-01T00:00:00Z"},
		{Name: "third", LastUpdated: "2026-01-01T00:00:00Z"},
	}
	sortTags(tags, "date", false)
	if tags[0].Name != "first" || tags[1].Name != "second" || tags[2].Name != "third" {
		t.Errorf("equal keys must keep order (stable): %v", names(tags))
	}
}

func TestRenderTagRows(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	out := renderTagRows(tags)
	for _, want := range []string{"Tag", "Status", "Size(MB)", "Architecture", "LastUpdated"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTagRows missing header %q:\n%s", want, out)
		}
	}
	// Size formatted to one decimal; date trimmed; arches joined.
	for _, want := range []string{"100.0", "50.0", "20.0", "2026-03-03", "amd64, arm64/v8", "inactive"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderTagRows missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "2026-03-03T") {
		t.Error("LastUpdated must be trimmed to date only")
	}
	if got := renderTagRows(nil); !strings.Contains(got, "Tag") {
		t.Error("empty input should still render the header")
	}
}

func TestBuildPullSpecs(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	specs := buildPullSpecs(tags, []string{"amd64", "arm64"})
	if len(specs) != 2 {
		t.Fatalf("got %d specs, want 2 (x86-only has neither arch)", len(specs))
	}
	if specs[0].Tag != "multi" || !specs[0].Arches["amd64"] || !specs[0].Arches["arm64"] {
		t.Errorf("multi spec = %+v", specs[0])
	}
	if specs[1].Tag != "amd-only" || !specs[1].Arches["amd64"] || specs[1].Arches["arm64"] {
		t.Errorf("amd-only spec must have amd64 only: %+v", specs[1])
	}
	if got := buildPullSpecs(tags, []string{"riscv64"}); len(got) != 0 {
		t.Errorf("no matching arch = %v, want none", got)
	}
	if got := buildPullSpecs(nil, []string{"amd64"}); len(got) != 0 {
		t.Errorf("nil tags = %v, want none", got)
	}
}

func TestExportTags_MarkdownContent(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	path := filepath.Join(t.TempDir(), "tags.md")

	if err := exportTags(path, "library", "nginx", tags, nil, false); err != nil {
		t.Fatalf("exportTags: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	md := string(data)
	for _, want := range []string{
		"# Docker Hub 镜像标签信息",
		"- 镜像: `library/nginx`",
		"- 标签数: 3",
		"| Tag | Status | Size(MB) | Architecture | LastUpdated |",
		"| multi | active | 100.0 | amd64, arm64/v8 | 2026-03-03 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "拉取命令") {
		t.Error("pull section must be absent without genPull")
	}
	if strings.Contains(md, "架构过滤") {
		t.Error("arch filter line must be absent without arch targets")
	}
}

func TestExportTags_WithArchAndGenPull(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	path := filepath.Join(t.TempDir(), "tags.md")

	err := exportTags(path, "bitnami", "nginx", tags, []string{"amd64", "arm64"}, true)
	if err != nil {
		t.Fatalf("exportTags: %v", err)
	}
	data, _ := os.ReadFile(path)
	md := string(data)
	for _, want := range []string{
		"- 架构过滤: amd64, arm64",
		"## Docker 镜像拉取命令",
		"bitnami/nginx:multi",
		"--platform=linux/arm64 bitnami/nginx:multi",
		"bitnami/nginx:amd-only",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("export missing %q:\n%s", want, md)
		}
	}
	// x86-only is still in the table, but must not appear in the pull section.
	pullSection := md[strings.Index(md, "## Docker 镜像拉取命令"):]
	if strings.Contains(pullSection, "x86-only") {
		t.Error("x86-only has no matched arch and must not appear in pull commands")
	}
}

func TestExportTags_GenPullWithoutArchOmitsSection(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	path := filepath.Join(t.TempDir(), "tags.md")
	if err := exportTags(path, "library", "nginx", tags, nil, true); err != nil {
		t.Fatalf("exportTags: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "拉取命令") {
		t.Error("genPull without arch targets must omit the pull section")
	}
}

func TestExportTags_WriteErrorPropagates(t *testing.T) {
	tags := fetchTagsFromMock(t, mockTagsBody, false)
	sentinel := errors.New("disk full")
	old := osWriteFile
	osWriteFile = func(string, []byte, os.FileMode) error { return sentinel }
	t.Cleanup(func() { osWriteFile = old })

	// exportTags itself returns the raw error; the RunE wraps it with
	// "writing output file" (covered in the e2e write-failure case).
	err := exportTags("unused.md", "u", "i", tags, nil, false)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the osWriteFile error", err)
	}
}
