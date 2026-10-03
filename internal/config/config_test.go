package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_MissingFileReturnsZeroNoError(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.Proxy != "" {
		t.Errorf("expected empty proxy, got %q", cfg.Proxy)
	}
}

func TestLoad_ReadsYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("proxy: \"http://from-file:1234\"\nno_color: true\n"), 0644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg.Proxy != "http://from-file:1234" {
		t.Errorf("proxy = %q, want http://from-file:1234", cfg.Proxy)
	}
	if !cfg.NoColor {
		t.Error("no_color should be true")
	}
}

func TestLoad_InvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("proxy: [unclosed"), 0644)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestEffectiveProxy_FlagOverridesFile(t *testing.T) {
	cfg := &Config{Proxy: "http://from-file:1234"}
	got := cfg.EffectiveProxy("http://from-flag:5678", "")
	want := "http://from-flag:5678"
	if got != want {
		t.Errorf("EffectiveProxy = %q, want %q", got, want)
	}
}

func TestEffectiveProxy_FallsBackToFile(t *testing.T) {
	cfg := &Config{Proxy: "http://from-file:1234"}
	got := cfg.EffectiveProxy("", "")
	if got != "http://from-file:1234" {
		t.Errorf("EffectiveProxy = %q, want file value", got)
	}
}

func TestEffectiveProxy_FallsBackToEnv(t *testing.T) {
	cfg := &Config{}
	os.Setenv("HTTPS_PROXY", "http://from-env:9999")
	defer os.Unsetenv("HTTPS_PROXY")
	got := cfg.EffectiveProxy("", "")
	if got != "http://from-env:9999" {
		t.Errorf("EffectiveProxy = %q, want env value", got)
	}
}

func TestEffectiveProxy_EnvHTTPSFallback(t *testing.T) {
	cfg := &Config{}
	os.Unsetenv("HTTPS_PROXY")
	os.Setenv("HTTP_PROXY", "http://from-http-env:1")
	defer os.Unsetenv("HTTP_PROXY")
	got := cfg.EffectiveProxy("", "")
	if got != "http://from-http-env:1" {
		t.Errorf("EffectiveProxy = %q, want http env value", got)
	}
}

func TestSave_RoundTripCreatesDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dhs", "config.yaml")
	want := &Config{Proxy: "http://p:1234", NoColor: true}
	if err := Save(path, want); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save failed: %v", err)
	}
	if got.Proxy != want.Proxy || got.NoColor != want.NoColor {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestDefaultConfigPath_ContainsDhs(t *testing.T) {
	p, err := DefaultConfigPath()
	if err != nil {
		t.Fatalf("DefaultConfigPath error: %v", err)
	}
	if filepath.Base(filepath.Dir(p)) != "dhs" {
		t.Errorf("default path %q should be inside a dhs dir", p)
	}
}
