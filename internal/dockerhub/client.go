package dockerhub

import (
	"fmt"
	"net/http"
	"net/url"
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

// BaseURL returns the Docker Hub API base URL.
func BaseURL() string { return baseURL }
