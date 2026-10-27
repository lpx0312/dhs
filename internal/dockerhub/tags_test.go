package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseTagArches_LinuxOnlyByDefault(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: "linux"},
			{Architecture: "arm64", Variant: "v8", OS: "linux"},
			{Architecture: "amd64", OS: "windows"},
		},
	}
	arches, archSet := parseTagArches(tr, false)
	// Default: linux only, variant appended.
	want := []string{"amd64", "arm64/v8"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v", arches, want)
	}
	// archSet holds linux architecture fields (no variant) for filtering.
	if !archSet["amd64"] || !archSet["arm64"] {
		t.Errorf("archSet = %v, want amd64+arm64", archSet)
	}
}

func TestParseTagArches_WithOSShowsNonLinux(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: "linux"},
			{Architecture: "amd64", OS: "windows"},
		},
	}
	arches, _ := parseTagArches(tr, true)
	// With OS: linux stays plain, windows gets os/ prefix.
	want := []string{"amd64", "windows/amd64"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v", arches, want)
	}
}

func TestParseTagArches_SkipsUnknown(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "unknown", OS: ""},
			{Architecture: "amd64", OS: "linux"},
		},
	}
	arches, _ := parseTagArches(tr, false)
	if len(arches) != 1 || arches[0] != "amd64" {
		t.Errorf("arches = %v, want [amd64]", arches)
	}
}

func TestParseTagArches_EmptyOSDefaultsLinux(t *testing.T) {
	tr := tagResult{
		Name: "latest",
		Images: []imageInfo{
			{Architecture: "amd64", OS: ""},
		},
	}
	arches, archSet := parseTagArches(tr, false)
	if len(arches) != 1 || arches[0] != "amd64" {
		t.Errorf("arches = %v, want [amd64]", arches)
	}
	if !archSet["amd64"] {
		t.Errorf("empty os should count as linux for archSet")
	}
}

func TestFetchTags_FollowsNextCursor(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		var pr paginatedTags
		if page == 1 {
			pr = paginatedTags{
				Next:    srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
				Results: []tagResult{{Name: "v1", TagStatus: "active", LastUpdated: "2026-01-01T00:00:00Z"}},
			}
		} else {
			pr = paginatedTags{
				Next:    "",
				Results: []tagResult{{Name: "v2", TagStatus: "active", LastUpdated: "2026-02-01T00:00:00Z"}},
			}
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	// limit=0 => fetch all pages. Reduce sleep to keep test fast.
	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 2 || tags[0].Name != "v1" || tags[1].Name != "v2" {
		t.Errorf("tags = %+v, want [v1 v2]", tags)
	}
}

func TestFetchTags_LimitStopsEarly(t *testing.T) {
	calls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		pr := paginatedTags{
			Next: srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
			Results: []tagResult{
				{Name: "a"}, {Name: "b"}, {Name: "c"},
			},
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 3, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 3 {
		t.Errorf("got %d tags, want 3", len(tags))
	}
	if calls != 1 {
		t.Errorf("got %d calls, want 1 (limit reached after first page)", calls)
	}
}

func TestFetchTags_404ReturnsNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "nope", "nope", 10, false)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

// --- 追加：fixture 多页、错误分支、归一化与 HasArch ---

func loadFixtureTags(t *testing.T, name string) paginatedTags {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var pr paginatedTags
	if err := json.Unmarshal(data, &pr); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return pr
}

// serveFixtureTags serves a named testdata tags fixture with Next cleared
// (single page, so the 500ms inter-page sleep never triggers).
func serveFixtureTags(t *testing.T, name string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pr := loadFixtureTags(t, name)
		pr.Next = ""
		json.NewEncoder(w).Encode(pr)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchTags_FixtureMultiPageInjectsNext(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		pr := loadFixtureTags(t, "tags_nginx_page2.json")
		if page == 1 {
			pr = loadFixtureTags(t, "tags_nginx_page1.json")
			pr.Next = srv.URL + "/v2/repositories/library/nginx/tags/?page=2"
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 12 {
		t.Errorf("got %d tags across pages, want 12", len(tags))
	}
	if tags[0].Name != "latest" || tags[10].Name != "1.25-alpine" || tags[11].Name != "1.25" {
		t.Errorf("page order broken: [0]=%s [10]=%s [11]=%s", tags[0].Name, tags[10].Name, tags[11].Name)
	}
}

func TestFetchTags_LimitBeyondOnePageFetchesSecond(t *testing.T) {
	page := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		pr := loadFixtureTags(t, "tags_nginx_page1.json") // 10 results
		if page == 1 {
			pr.Next = srv.URL + "/v2/repositories/library/nginx/tags/?page=2"
		}
		json.NewEncoder(w).Encode(pr)
	}))
	defer srv.Close()

	prevSleep := tagsPageSleep
	tagsPageSleep = 0
	defer func() { tagsPageSleep = prevSleep }()

	// limit 11: one page (10) is not enough, the second page is fetched.
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 11, false)
	if err != nil {
		t.Fatalf("FetchTags error: %v", err)
	}
	if len(tags) != 20 || page != 2 {
		t.Errorf("got %d tags in %d pages, want 20 in 2", len(tags), page)
	}
}

func TestFetchTags_FullSizeConvertedToMB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{
			Results: []tagResult{{Name: "mib", FullSize: 50 * 1024 * 1024}},
		})
	}))
	defer srv.Close()
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "x", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tags[0].SizeMB != 50.0 {
		t.Errorf("SizeMB = %v, want 50.0", tags[0].SizeMB)
	}
}

func TestFetchTags_StatusAndLastUpdatedPassThrough(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if tags[0].Status != "active" {
		t.Errorf("status = %q", tags[0].Status)
	}
	if !strings.HasPrefix(tags[0].LastUpdated, "2026-08-01T08:00:00") {
		t.Errorf("last_updated = %q", tags[0].LastUpdated)
	}
	// alpine-slim carries the inactive status from the fixture.
	for _, tg := range tags {
		if tg.Name == "alpine-slim" && tg.Status != "inactive" {
			t.Errorf("alpine-slim status = %q, want inactive", tg.Status)
		}
	}
}

func TestFetchTags_HasArchFromFixture(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	byName := map[string]Tag{}
	for _, tg := range tags {
		byName[tg.Name] = tg
	}
	// latest: amd64 + arm64 + arm/v7 + 386 on linux; windows/amd64 excluded from set.
	for _, a := range []string{"amd64", "arm64", "arm", "386"} {
		if !byName["latest"].HasArch(a) {
			t.Errorf("latest should report arch %s", a)
		}
	}
	// alpine: 386 only.
	if byName["alpine"].HasArch("amd64") || !byName["alpine"].HasArch("386") {
		t.Error("alpine arch set should be exactly {386}")
	}
	// stable-alpine: arm64 only.
	if byName["stable-alpine"].HasArch("amd64") || !byName["stable-alpine"].HasArch("arm64") {
		t.Error("stable-alpine arch set should be exactly {arm64}")
	}
	// perl: windows-only => empty linux arch set by default.
	if byName["perl"].HasArch("amd64") {
		t.Error("windows-only tag must not report amd64 in linux arch set")
	}
	// Display list excludes windows arch by default.
	if strings.Contains(strings.Join(byName["latest"].Arches, ","), "windows") {
		t.Errorf("latest Arches = %v, must not include windows by default", byName["latest"].Arches)
	}
}

func TestFetchTags_ShowOSSurfacesWindowsArches(t *testing.T) {
	srv := serveFixtureTags(t, "tags_nginx_page1.json")
	tags, err := FetchTags(srv.Client(), srv.URL, "library", "nginx", 0, true)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	for _, tg := range tags {
		if tg.Name == "latest" {
			joined := strings.Join(tg.Arches, ",")
			if !strings.Contains(joined, "windows/amd64") {
				t.Errorf("latest with showOS = %v, want windows/amd64", tg.Arches)
			}
			if !tg.HasArch("amd64") {
				t.Error("showOS must still populate linux archSet for filtering")
			}
		}
		if tg.Name == "perl" {
			// windows-only tag surfaces with prefix and no bare linux arch.
			if len(tg.Arches) != 1 || tg.Arches[0] != "windows/amd64" {
				t.Errorf("perl with showOS = %v, want [windows/amd64]", tg.Arches)
			}
		}
	}
}

func TestFetchTags_ServerError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500 error", err)
	}
}

func TestFetchTags_InvalidJSONReturnsParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"count":1,"results":[bad`))
	}))
	defer srv.Close()
	_, err := FetchTags(srv.Client(), srv.URL, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("err = %v, want parsing error", err)
	}
}

func TestFetchTags_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := FetchTags(srv.Client(), url, "u", "i", 10, false)
	if err == nil || !strings.Contains(err.Error(), "tags request failed") {
		t.Errorf("err = %v, want transport error", err)
	}
}

func TestFetchTags_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(paginatedTags{Count: 0, Results: []tagResult{}})
	}))
	defer srv.Close()
	tags, err := FetchTags(srv.Client(), srv.URL, "u", "i", 0, false)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("got %d tags, want 0", len(tags))
	}
}

func TestHasArch_EmptySetIsFalse(t *testing.T) {
	tg := Tag{Name: "x"}
	if tg.HasArch("amd64") {
		t.Error("empty arch set must report false")
	}
}

func TestParseTagArches_VariantComposedAndSorted(t *testing.T) {
	tr := tagResult{Images: []imageInfo{
		{Architecture: "arm64", Variant: "v8", OS: "linux"},
		{Architecture: "amd64", OS: "linux"},
		{Architecture: "arm", Variant: "v7", OS: "linux"},
	}}
	arches, archSet := parseTagArches(tr, false)
	want := []string{"amd64", "arm/v7", "arm64/v8"}
	if !reflect.DeepEqual(arches, want) {
		t.Errorf("arches = %v, want %v (sorted, variant kept)", arches, want)
	}
	for _, a := range []string{"amd64", "arm", "arm64"} {
		if !archSet[a] {
			t.Errorf("archSet missing %s (variant must be stripped)", a)
		}
	}
}

func TestParseTagArches_EmptyImages(t *testing.T) {
	arches, archSet := parseTagArches(tagResult{}, false)
	if len(arches) != 0 || len(archSet) != 0 {
		t.Errorf("no images => empty lists, got %v %v", arches, archSet)
	}
}
