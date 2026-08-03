package dockerhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
