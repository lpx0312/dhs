package dockerhub

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestNewClient_NoProxyUsesEnvironmentProxy(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.Transport)
	}
	// With empty proxy we use ProxyFromEnvironment.
	if tr.Proxy == nil {
		t.Error("expected non-nil Proxy function")
	}
}

func TestNewClient_WithProxySetsExplicitProxyURL(t *testing.T) {
	c, err := NewClient("http://127.0.0.1:7892")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	tr := c.Transport.(*http.Transport)
	req := &http.Request{URL: &url.URL{Scheme: "https", Host: "hub.docker.com"}}
	got, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("Proxy() error: %v", err)
	}
	if got.String() != "http://127.0.0.1:7892" {
		t.Errorf("proxy = %q, want http://127.0.0.1:7892", got)
	}
}

func TestNewClient_InvalidProxyReturnsError(t *testing.T) {
	_, err := NewClient("://bad-url")
	if err == nil {
		t.Fatal("expected error for invalid proxy URL, got nil")
	}
}

func TestNewClient_ProxyWithoutSchemeReturnsError(t *testing.T) {
	// "host:port" parses fine but has no scheme: must be rejected.
	_, err := NewClient("127.0.0.1:7897")
	if err == nil {
		t.Fatal("expected missing-scheme error, got nil")
	}
}

func TestNewClient_TimeoutApplied(t *testing.T) {
	c, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	if c.Timeout != 30*time.Second {
		t.Errorf("client timeout = %v, want 30s", c.Timeout)
	}
}

func TestBaseURL_Default(t *testing.T) {
	t.Setenv("DHS_API_BASE", "")
	if got := BaseURL(); got != "https://hub.docker.com" {
		t.Errorf("BaseURL() = %q, want default hub.docker.com", got)
	}
}

func TestBaseURL_EnvOverride(t *testing.T) {
	t.Setenv("DHS_API_BASE", "http://127.0.0.1:19999")
	if got := BaseURL(); got != "http://127.0.0.1:19999" {
		t.Errorf("BaseURL() = %q, want env override", got)
	}
}
