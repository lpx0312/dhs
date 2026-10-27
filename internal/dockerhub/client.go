package dockerhub

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const baseURL = "https://hub.docker.com"

// NewClient builds an *http.Client. If proxyURL is empty, the transport
// uses ProxyFromEnvironment (reads HTTPS_PROXY/HTTP_PROXY), matching the
// ps1 script's reliance on env vars. If proxyURL is set, an explicit
// proxy is wired in and env vars are ignored to avoid ambiguity.
func NewClient(proxyURL string) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
	}
	if proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", proxyURL, err)
		}
		if u.Scheme == "" {
			return nil, fmt.Errorf("invalid proxy URL %q: missing scheme", proxyURL)
		}
		transport.Proxy = http.ProxyURL(u)
	} else {
		transport.Proxy = http.ProxyFromEnvironment
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// BaseURL returns the Docker Hub API base URL. The DHS_API_BASE
// environment variable overrides it, which lets e2e tests point the CLI
// at a mock server and users point it at a compatible mirror. A trailing
// slash is trimmed so callers can concatenate base + "/v2/..." without
// producing "//v2/..." double-slash paths.
func BaseURL() string {
	if v := os.Getenv("DHS_API_BASE"); v != "" {
		return strings.TrimSuffix(v, "/")
	}
	return baseURL
}
