package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetchRepositories_SinglePageReturnsResults(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		resp := paginatedRepos{
			Count: 2,
			Results: []repoResult{
				{Name: "nginx", StarCount: 20000, PullCount: 10000000, IsOfficial: true, ShortDescription: "Official build of Nginx."},
				{Name: "bitnami/nginx", StarCount: 300, PullCount: 50000000, IsOfficial: false, ShortDescription: "Bitnami nginx image"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := srv.Client()
	repos, total, err := FetchRepositories(client, srv.URL, "nginx", 25)
	if err != nil {
		t.Fatalf("FetchRepositories error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos, want 2", len(repos))
	}
	if repos[0].Name != "nginx" || !repos[0].Official {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	// Verify page_size query param set to limit.
	parsed, _ := url.Parse(capturedURL)
	if parsed.Query().Get("page_size") != "25" {
		t.Errorf("page_size = %q, want 25", parsed.Query().Get("page_size"))
	}
	if parsed.Query().Get("query") != "nginx" {
		t.Errorf("query = %q, want nginx", parsed.Query().Get("query"))
	}
}

func TestFetchRepositories_HTTPErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 25)
	if err == nil {
		t.Fatal("expected error for 429, got nil")
	}
}

// --- 追加：mock fixture、默认值与错误分支 ---

// serveFixture starts a server replying with the named testdata file (or
// raw when the body is inline JSON), recording the last request URL.
func serveFixture(t *testing.T, name, raw string, status int) (*httptest.Server, *string) {
	t.Helper()
	captured := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.String()
		if status != 0 {
			w.WriteHeader(status)
		}
		var body []byte
		if name != "" {
			body, _ = os.ReadFile(filepath.Join("testdata", name))
		} else {
			body = []byte(raw)
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func TestFetchRepositories_FromFixture(t *testing.T) {
	srv, _ := serveFixture(t, "search_nginx.json", "", 0)
	repos, total, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 25)
	if err != nil {
		t.Fatalf("FetchRepositories error: %v", err)
	}
	if total != 3 || len(repos) != 3 {
		t.Fatalf("total=%d len=%d, want 3/3", total, len(repos))
	}
	if repos[0].Name != "nginx" || !repos[0].Official || repos[0].Stars != 20000 {
		t.Errorf("repos[0] = %+v", repos[0])
	}
	if repos[1].Name != "bitnami/nginx" || repos[1].Official {
		t.Errorf("repos[1] = %+v", repos[1])
	}
	if repos[2].Description != "Unprivileged NGINX image" {
		t.Errorf("repos[2] description = %q", repos[2].Description)
	}
}

func TestFetchRepositories_NonPositiveLimitDefaultsTo25(t *testing.T) {
	for _, limit := range []int{0, -5} {
		srv, captured := serveFixture(t, "search_nginx.json", "", 0)
		if _, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", limit); err != nil {
			t.Fatalf("limit=%d: %v", limit, err)
		}
		parsed, _ := url.Parse(*captured)
		if got := parsed.Query().Get("page_size"); got != "25" {
			t.Errorf("limit=%d: page_size = %q, want 25", limit, got)
		}
	}
}

func TestFetchRepositories_KeywordIsEscaped(t *testing.T) {
	srv, captured := serveFixture(t, "search_nginx.json", "", 0)
	if _, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx alpine&co", 10); err != nil {
		t.Fatalf("error: %v", err)
	}
	parsed, _ := url.Parse(*captured)
	if got := parsed.Query().Get("query"); got != "nginx alpine&co" {
		t.Errorf("decoded query = %q, want original keyword", got)
	}
}

func TestFetchRepositories_EmptyResults(t *testing.T) {
	srv, _ := serveFixture(t, "", `{"count":0,"next":null,"results":[]}`, 0)
	repos, total, err := FetchRepositories(srv.Client(), srv.URL, "zzzznothing", 10)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if total != 0 || len(repos) != 0 {
		t.Errorf("total=%d repos=%v, want 0/empty", total, repos)
	}
}

func TestFetchRepositories_InvalidJSONReturnsParseError(t *testing.T) {
	srv, _ := serveFixture(t, "", `{"count": 1, "results": [not-json`, 0)
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "parsing") {
		t.Errorf("err = %v, want parsing error", err)
	}
}

func TestFetchRepositories_ServerError500(t *testing.T) {
	srv, _ := serveFixture(t, "", "", http.StatusInternalServerError)
	_, _, err := FetchRepositories(srv.Client(), srv.URL, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v, want HTTP 500 error", err)
	}
}

func TestFetchRepositories_TransportError(t *testing.T) {
	srv, _ := serveFixture(t, "", "", 0)
	url := srv.URL
	srv.Close() // force a connection failure on the next request
	_, _, err := FetchRepositories(srv.Client(), url, "nginx", 10)
	if err == nil || !strings.Contains(err.Error(), "search request failed") {
		t.Errorf("err = %v, want transport error", err)
	}
}
