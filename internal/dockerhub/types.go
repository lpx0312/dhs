package dockerhub

// imageInfo is one platform variant under a tag.
type imageInfo struct {
	Architecture string `json:"architecture"`
	Variant      string `json:"variant"`
	OS           string `json:"os"`
}

// tagResult is one tag entry from the /tags/ endpoint.
type tagResult struct {
	Name        string      `json:"name"`
	TagStatus   string      `json:"tag_status"`
	FullSize    int64       `json:"full_size"`
	LastUpdated string      `json:"last_updated"`
	Images      []imageInfo `json:"images"`
}

// paginatedTags is the raw /tags/ response envelope.
type paginatedTags struct {
	Count    int         `json:"count"`
	Next     string      `json:"next"`
	Previous string      `json:"previous"`
	Results  []tagResult `json:"results"`
}

// repoResult is one repository from the /search/repositories/ endpoint.
// Note: the search API uses "repo_name" (not "name") and does NOT return
// last_updated — only the /repositories/{u}/{r} detail endpoint does.
type repoResult struct {
	Name             string `json:"repo_name"`
	StarCount        int64  `json:"star_count"`
	PullCount        int64  `json:"pull_count"`
	IsOfficial       bool   `json:"is_official"`
	ShortDescription string `json:"short_description"`
}

// paginatedRepos is the raw /search/repositories/ response envelope.
type paginatedRepos struct {
	Count   int          `json:"count"`
	Next    string       `json:"next"`
	Results []repoResult `json:"results"`
}

// --- Public, normalized types returned to callers ---

// Tag is the normalized tag info surfaced to the UI layer.
type Tag struct {
	Name        string
	Status      string
	SizeMB      float64
	Arches      []string // display list (linux-only by default, or os/arch with ShowOS)
	LastUpdated string
	// archSet is the set of linux architecture fields (no variant) for filtering.
	archSet map[string]bool
}

// Repo is the normalized repository info surfaced to the UI layer.
type Repo struct {
	Name        string
	Stars       int64
	Pulls       int64
	Official    bool
	Description string
	LastUpdated string
}
