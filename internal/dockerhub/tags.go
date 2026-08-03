package dockerhub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"
)

// tagsPageSleep is the delay between pages. Overridable in tests.
var tagsPageSleep = 500 * time.Millisecond

// FetchTags retrieves tags for user/image. It pages through results
// (page_size 100) following the API's next cursor until exhausted.
// If limit > 0, fetching stops once at least limit tags are collected.
// Set limit to 0 to fetch all pages. showOS controls whether non-linux
// architectures are surfaced (with an os/ prefix) in the display list.
func FetchTags(client *http.Client, base, user, image string, limit int, showOS bool) ([]Tag, error) {
	u := fmt.Sprintf("%s/v2/repositories/%s/%s/tags/?page_size=100", base, user, image)
	var out []Tag

	for {
		resp, err := client.Get(u)
		if err != nil {
			return nil, fmt.Errorf("tags request failed: %w", err)
		}
		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return nil, fmt.Errorf("image %s/%s not found", user, image)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, fmt.Errorf("docker hub tags returned HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading tags response: %w", err)
		}
		var pr paginatedTags
		if err := json.Unmarshal(body, &pr); err != nil {
			return nil, fmt.Errorf("parsing tags response: %w", err)
		}
		for _, tr := range pr.Results {
			arches, archSet := parseTagArches(tr, showOS)
			out = append(out, Tag{
				Name:        tr.Name,
				Status:      tr.TagStatus,
				SizeMB:      float64(tr.FullSize) / 1024.0 / 1024.0,
				Arches:      arches,
				LastUpdated: tr.LastUpdated,
				archSet:     archSet,
			})
		}
		if pr.Next == "" {
			break
		}
		if limit > 0 && len(out) >= limit {
			break
		}
		u = pr.Next
		time.Sleep(tagsPageSleep) // polite rate limit, matches ps1
	}
	return out, nil
}

// parseTagArches builds the display architecture list and the filtering set.
// showOS mirrors ps1's -ShowOS: when true, non-linux arches get an "os/" prefix.
// archSet holds linux architecture fields (without variant) for --arch filtering.
func parseTagArches(tr tagResult, showOS bool) ([]string, map[string]bool) {
	seen := map[string]bool{}
	archSet := map[string]bool{}
	for _, img := range tr.Images {
		if img.Architecture == "" || img.Architecture == "unknown" {
			continue
		}
		os := img.OS
		if os == "" {
			os = "linux"
		}
		arch := img.Architecture
		if img.Variant != "" {
			arch = arch + "/" + img.Variant
		}
		if showOS {
			if os != "linux" {
				arch = os + "/" + arch
			}
			seen[arch] = true
		} else {
			// Default: linux arches only.
			if os == "linux" {
				seen[arch] = true
				archSet[img.Architecture] = true
			}
		}
		// archSet always tracks linux architecture fields for filtering,
		// even when showOS is true (so --arch + --os compose correctly).
		if showOS && os == "linux" {
			archSet[img.Architecture] = true
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out, archSet
}

// HasArch reports whether the tag's linux architecture set contains target.
func (t Tag) HasArch(target string) bool {
	return t.archSet[target]
}
