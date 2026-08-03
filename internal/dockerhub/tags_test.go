package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
				Next: srv.URL + "/v2/repositories/library/nginx/tags/?page=2",
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
