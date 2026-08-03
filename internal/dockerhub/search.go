package dockerhub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// FetchRepositories searches Docker Hub repositories for the keyword.
// It performs a single request with page_size=limit (search is ranked;
// top-N paging is sufficient and matches the design spec).
// Returns the normalized repos and the total count reported by the API.
func FetchRepositories(client *http.Client, base, keyword string, limit int) ([]Repo, int, error) {
	if limit <= 0 {
		limit = 25
	}
	u := base + "/v2/search/repositories/?query=" + url.QueryEscape(keyword) +
		"&page_size=" + fmt.Sprintf("%d", limit)

	resp, err := client.Get(u)
	if err != nil {
		return nil, 0, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("docker hub search returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("reading search response: %w", err)
	}

	var pr paginatedRepos
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, 0, fmt.Errorf("parsing search response: %w", err)
	}

	out := make([]Repo, 0, len(pr.Results))
	for _, r := range pr.Results {
		out = append(out, Repo{
			Name:        r.Name,
			Stars:       r.StarCount,
			Pulls:       r.PullCount,
			Official:    r.IsOfficial,
			Description: r.ShortDescription,
		})
	}
	return out, pr.Count, nil
}
