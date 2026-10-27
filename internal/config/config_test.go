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

// --- 追加：边界与错误分支 ---

func TestLoad_EmptyPathReturnsZero(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error: %v", err)
	}
	if cfg == nil || cfg.Proxy != "" || cfg.NoColor {
		t.Errorf("Load(\"\") = %+v, want zero config", cfg)
	}
}

func TestLoad_EmptyFileContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, nil, 0644)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("empty file should load, got %v", err)
	}
	if cfg.Proxy != "" || cfg.NoColor {
		t.Errorf("empty file = %+v, want zero config", cfg)
	}
}

func TestLoad_DirectoryPathReturnsReadError(t *testing.T) {
	// A directory is neither missing nor valid YAML: exercises the
	// non-IsNotExist error branch of the ReadFile call.
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected read error for directory path, got nil")
	}
}

func TestLoad_TypeMismatchReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("proxy: [a, b]\n"), 0644) // sequence where string expected
	if _, err := Load(path); err == nil {
		t.Fatal("expected unmarshal error for type mismatch")
	}
}

func TestEffectiveNoColor(t *testing.T) {
	cases := []struct {
		cfg  Config
		flag bool
		want bool
	}{
		{Config{}, false, false},
		{Config{}, true, true},
		{Config{NoColor: true}, false, true},
		{Config{NoColor: true}, true, true},  // flag wins, same value
		{Config{NoColor: false}, true, true}, // flag wins over file
	}
	for i, c := range cases {
		if got := c.cfg.EffectiveNoColor(c.flag); got != c.want {
			t.Errorf("case %d: EffectiveNoColor(%v) = %v, want %v", i, c.flag, got, c.want)
		}
	}
}

func TestEffectiveProxy_DirectWhenNothingSet(t *testing.T) {
	for _, k := range []string{"HTTPS_PROXY", "HTTP_PROXY"} {
		old, had := os.LookupEnv(k)
		os.Unsetenv(k)
		defer func() {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		}()
	}
	cfg := &Config{}
	if got := cfg.EffectiveProxy("", ""); got != "" {
		t.Errorf("EffectiveProxy = %q, want empty (direct)", got)
	}
}

func TestSave_ParentIsFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// "blocker/config.yaml": MkdirAll must fail because a path element
	// exists as a regular file.
	err := Save(filepath.Join(blocker, "config.yaml"), &Config{})
	if err == nil {
		t.Fatal("expected mkdir error when parent path is a file")
	}
}

func TestSave_PathIsDirectoryReturnsWriteError(t *testing.T) {
	// Writing "into" an existing directory must fail at the WriteFile step.
	err := Save(t.TempDir(), &Config{})
	if err == nil {
		t.Fatal("expected write error when target is a directory")
	}
}

func TestSave_EmptyConfigRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dhs", "config.yaml")
	if err := Save(path, &Config{}); err != nil {
		t.Fatalf("Save empty config: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Proxy != "" || got.NoColor {
		t.Errorf("round trip = %+v, want zero config", got)
	}
}

func TestDefaultConfigPath_ErrorWhenUserDirUnresolvable(t *testing.T) {
	// Blank every variable os.UserConfigDir consults so it must fail.
	for _, k := range []string{"AppData", "XDG_CONFIG_HOME", "HOME"} {
		old, had := os.LookupEnv(k)
		os.Setenv(k, "")
		defer func(k, old string, had bool) {
			if had {
				os.Setenv(k, old)
			} else {
				os.Unsetenv(k)
			}
		}(k, old, had)
	}
	if _, err := DefaultConfigPath(); err == nil {
		t.Fatal("expected error when user config dir cannot be resolved")
	}
}
