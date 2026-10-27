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
	"dhs/internal/ui"
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

		// --- 【绿·钉住畸形现状】以下用例钉住 parseImageRef 对 tag/digest/registry
		// 引用的错误切分。修复方向（报错 vs 剥离 tag/digest vs 识别 registry 前缀）
		// 未定，先钉住当前行为防止静默变化；修复落地时这些期望值应同步翻转。 ---
		// 带 tag 的官方镜像：tag 未剥离，整体进 name。
		{"nginx:1.27", "library", "nginx:1.27"},
		// 带 tag 的用户镜像：tag 同样未剥离。
		{"bitnami/nginx:1.27", "bitnami", "nginx:1.27"},
		// digest 引用：无斜杠，digest 整体留在 name 里。
		{"nginx@sha256:abcd1234", "library", "nginx@sha256:abcd1234"},
		// 带 registry（含端口）的引用：第一个斜杠切分，host:port 被当成 user，
		// "team/app" 被当成镜像名（其内部的斜杠未再处理）。
		{"registry.io:5000/team/app", "registry.io:5000", "team/app"},
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

// 【绿·钉住现状】sortTags 补 size desc 组合（原测试只覆盖 size asc）：
// 大 size 在前。
func TestSortTags_SizeDesc(t *testing.T) {
	tags := []dockerhub.Tag{
		{Name: "small", SizeMB: 5},
		{Name: "big", SizeMB: 50},
		{Name: "mid", SizeMB: 20},
	}
	sortTags(tags, "size", false)
	if tags[0].Name != "big" || tags[1].Name != "mid" || tags[2].Name != "small" {
		t.Errorf("size desc = %v, want big>mid>small", names(tags))
	}
}

// 【绿·钉住现状】--sort 非法值走 default(date) 分支，且 switch 大小写敏感：
// "size "（尾随空格）、"NAME"（大写）都不命中 name/size，落回 date。
// 命名让 date-asc 与 name-asc 的期望顺序不同，从而证明确实走了 date 分支。
func TestSortTags_IllegalSortKeyFallsBackToDate_GreenPin(t *testing.T) {
	tags := []dockerhub.Tag{
		{Name: "aaa-new", SizeMB: 1, LastUpdated: "2026-06-01T00:00:00Z"},
		{Name: "zzz-old", SizeMB: 9, LastUpdated: "2026-01-01T00:00:00Z"},
	}
	// 前置 sanity：name asc → aaa-new 在前。
	sortTags(tags, "name", true)
	if tags[0].Name != "aaa-new" {
		t.Fatalf("sanity name asc = %v", names(tags))
	}
	// 非法 "size "（含空格）+ desc → date desc：新的在前。
	sortTags(tags, "size ", false)
	if tags[0].Name != "aaa-new" || tags[1].Name != "zzz-old" {
		t.Errorf("非法 sort 键 %q 应走 date desc = %v", "size ", names(tags))
	}
	// "NAME"（大写）+ asc：若命中 name 分支应为 aaa-new 在前；实际走 date asc
	// → 旧的在前，两者顺序不同，可区分。
	sortTags(tags, "NAME", true)
	if tags[0].Name != "zzz-old" || tags[1].Name != "aaa-new" {
		t.Errorf("大写 %q 不命中 name 分支，应走 date asc = %v", "NAME", names(tags))
	}
}

// mockTwoTagTagsBody：两个仅日期不同的 tag，供 RunE 排序方向断言用。
const mockTwoTagTagsBody = `{
 "count": 2, "next": "", "results": [
  {"name":"oldtag","tag_status":"active","full_size":1048576,"last_updated":"2026-01-01T00:00:00Z",
   "images":[{"architecture":"amd64","os":"linux"}]},
  {"name":"newtag","tag_status":"active","full_size":2097152,"last_updated":"2026-06-01T00:00:00Z",
   "images":[{"architecture":"amd64","os":"linux"}]}
 ]}`

// 【绿·钉住现状】--order 非法值被当 desc：RunE 以 tagsOrder == "asc" 计算
// 升序标记，除 "asc" 外的任何值（含拼错的 "bogus"）都等价 desc。
// 直调 tagsCmd.RunE + 本地 httptest mock（127.0.0.1，离线安全），不经 cobra。
func TestTagsRunE_IllegalOrderTreatedAsDesc_GreenPin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(mockTwoTagTagsBody))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("DHS_API_BASE", srv.URL)
	// 直连 mock；屏蔽开发 shell 可能导出的代理环境变量。
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "")

	// 快照并恢复 tags RunE 读写的全部包级 flag 变量。
	oldConfig, oldProxy, oldNoColor := flagConfig, flagProxy, flagNoColor
	oldLimit, oldSort, oldOrder := tagsLimit, tagsSort, tagsOrder
	oldAll, oldOS, oldFilter := tagsAll, tagsOS, tagsFilter
	oldArch, oldGenPull, oldOutput := tagsArch, tagsGenPull, tagsOutput
	t.Cleanup(func() {
		flagConfig, flagProxy, flagNoColor = oldConfig, oldProxy, oldNoColor
		tagsLimit, tagsSort, tagsOrder = oldLimit, oldSort, oldOrder
		tagsAll, tagsOS, tagsFilter = oldAll, oldOS, oldFilter
		tagsArch, tagsGenPull, tagsOutput = oldArch, oldGenPull, oldOutput
		ui.SetEnabled(false) // RunE 内 applyColorSetting 会翻转全局色彩开关
	})

	run := func(order string) string {
		flagConfig = filepath.Join(t.TempDir(), "config.yaml")
		flagProxy, flagNoColor = "", false
		tagsLimit, tagsSort, tagsAll, tagsOS = 10, "date", false, false
		tagsFilter, tagsArch, tagsGenPull, tagsOutput = "", "", false, ""
		tagsOrder = order
		return captureRunEOutput(t, func() {
			if err := tagsCmd.RunE(nil, []string{"demo"}); err != nil {
				t.Errorf("tagsCmd.RunE(order=%q): %v", order, err)
			}
		})
	}

	rowOrder := func(out string) (int, int) {
		iOld := strings.Index(out, "\noldtag ")
		iNew := strings.Index(out, "\nnewtag ")
		if iOld < 0 || iNew < 0 {
			t.Fatalf("输出缺少数据行:\n%s", out)
		}
		return iOld, iNew
	}

	// 非法 order 值 → desc：新的（newtag, 2026-06）在前。
	out := run("bogus")
	iOld, iNew := rowOrder(out)
	if iNew > iOld {
		t.Errorf("--order bogus 当前被当作 desc，newtag 应排在 oldtag 前:\n%s", out)
	}
	// 对照：精确的 "asc" 才会翻转为升序。
	out = run("asc")
	iOld, iNew = rowOrder(out)
	if iNew < iOld {
		t.Errorf("--order asc 应为升序，oldtag 应排在 newtag 前:\n%s", out)
	}
}

// emptyArchTagsBody：一个 tag 无 images，另一个只有 "unknown" 架构。
// 两者经 FetchTags 规范化后 archSet 均为空集。
const emptyArchTagsBody = `{
 "count": 2, "next": "", "results": [
  {"name":"no-images","tag_status":"active","full_size":1024,"last_updated":"2026-01-01T00:00:00Z",
   "images":[]},
  {"name":"unknown-only","tag_status":"active","full_size":2048,"last_updated":"2026-01-02T00:00:00Z",
   "images":[{"architecture":"unknown","os":"linux"}]}
 ]}`

// 【绿·钉住现状】filterByArch 把"空架构集"视为不匹配：
// images 为空或全为 unknown 的 tag，archSet 为空 → HasArch 恒 false → 被过滤掉，
// 即使目标架构列表非空也不会"匹配任何架构"。必须经 FetchTags 构造
// （archSet 未导出），故复用 fetchTagsFromMock。
func TestFilterByArch_EmptyArchSetNeverMatches_GreenPin(t *testing.T) {
	tags := fetchTagsFromMock(t, emptyArchTagsBody, false)
	if len(tags) != 2 {
		t.Fatalf("mock 应返回 2 个 tag, got %d", len(tags))
	}
	for _, tg := range tags {
		if tg.HasArch("amd64") {
			t.Errorf("%s: 空架构集 HasArch(amd64) 应为 false", tg.Name)
		}
	}
	if got := filterByArch(tags, []string{"amd64"}); len(got) != 0 {
		t.Errorf("单目标过滤 = %v, want 空（空架构集不匹配）", names(got))
	}
	if got := filterByArch(tags, []string{"amd64", "arm64", "386"}); len(got) != 0 {
		t.Errorf("多目标过滤 = %v, want 空", names(got))
	}
}
