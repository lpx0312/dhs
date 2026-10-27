package dockerhub

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"
)

// tagsPageSleep is the delay between pages. Overridable in tests.
var tagsPageSleep = 500 * time.Millisecond

// maxPages is the hard cap on pages fetched per FetchTags call. It is the
// backstop behind the visited-set loop detection: a hostile mirror can
// return a fresh, never-repeating cursor on every page and would otherwise
// paginate (and grow the visited map) forever. Overridable in tests.
var maxPages = 1000

// FetchTags retrieves tags for user/image. It pages through results
// (page_size 100) following the API's next cursor until exhausted.
// If limit > 0, fetching stops once at least limit tags are collected.
// Set limit to 0 to fetch all pages. showOS controls whether non-linux
// architectures are surfaced (with an os/ prefix) in the display list.
// The next cursor is resolved against base (relative cursors) and must
// stay on the base's scheme, host and userinfo; already-visited URLs are
// rejected to prevent exact-repeat pagination loops, and maxPages caps
// the total page count regardless of cursor shape.
func FetchTags(client *http.Client, base, user, image string, limit int, showOS bool) ([]Tag, error) {
	u := fmt.Sprintf("%s/v2/repositories/%s/%s/tags/?page_size=100",
		base, url.PathEscape(user), url.PathEscape(image))
	var out []Tag
	visited := map[string]bool{}
	pages := 0

	for {
		if visited[u] {
			return nil, fmt.Errorf("tags pagination loop detected: next cursor points back to already-fetched URL %s", redactURL(u))
		}
		visited[u] = true
		pages++
		if pages > maxPages {
			return nil, fmt.Errorf("tags pagination exceeded %d pages (last URL %s)", maxPages, redactURL(u))
		}
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
			return nil, fmt.Errorf("docker hub tags returned HTTP %d for %s", resp.StatusCode, redactURL(u))
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
		next, err := resolveNext(base, pr.Next)
		if err != nil {
			return nil, err
		}
		u = next
		time.Sleep(tagsPageSleep) // polite rate limit, matches ps1
	}
	return out, nil
}

// resolveNext turns the API's next cursor into an absolute URL. The cursor
// is first resolved against base (so plain relative cursors such as
// "/v2/...?page=2" work), and the resulting absolute URL must then match
// the configured base exactly on scheme, host and userinfo. Any deviation —
// a protocol-relative "//host/..." cursor, a scheme mismatch such as an
// http cursor against an https base, a foreign host, or injected userinfo —
// means the request would leave the configured API base (e.g. a malicious
// mirror response) and is rejected. URLs in the error are redacted so
// credentials never leak into CLI output.
func resolveNext(base, next string) (string, error) {
	nextURL, err := url.Parse(next)
	if err != nil {
		return "", fmt.Errorf("parsing tags next cursor %q: %w", next, err)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parsing base URL %q: %w", base, err)
	}
	resolved := baseURL.ResolveReference(nextURL)
	if resolved.Scheme != baseURL.Scheme || resolved.Host != baseURL.Host || userinfoOf(resolved) != userinfoOf(baseURL) {
		return "", fmt.Errorf("tags next cursor %s deviates from the configured base %s (scheme, host and userinfo must match); refusing to follow", redactURL(resolved.String()), redactURL(base))
	}
	return resolved.String(), nil
}

// userinfoOf renders u's userinfo as a comparable string ("" when absent).
// A cursor may not carry userinfo differing from the base's own: ResolveReference
// inherits the base userinfo for plain relative cursors, so those still pass.
func userinfoOf(u *url.URL) string {
	if u.User == nil {
		return ""
	}
	return u.User.String()
}

// redactURL renders u for error messages with any password masked
// (u.Redacted()), so credentials embedded in a DHS_API_BASE mirror URL or
// in a next cursor never leak into CLI output. Strings that fail to parse
// are returned verbatim.
func redactURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return u
	}
	return parsed.Redacted()
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
