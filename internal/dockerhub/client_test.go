package dockerhub

import (
	"net/http"
	"net/url"
	"testing"
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
