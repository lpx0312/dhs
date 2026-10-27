package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/config"
	"dhs/internal/ui"
)

// --- configPath / loadConfig ---

func TestConfigPath_FlagWins(t *testing.T) {
	flagConfig = "/custom/path.yaml"
	t.Cleanup(func() { flagConfig = "" })
	got, err := configPath()
	if err != nil || got != "/custom/path.yaml" {
		t.Errorf("configPath() = %q, %v; want flag value", got, err)
	}
}

func TestConfigPath_DefaultContainsDhs(t *testing.T) {
	flagConfig = ""
	got, err := configPath()
	if err != nil {
		t.Fatalf("configPath error: %v", err)
	}
	if !strings.Contains(filepath.ToSlash(got), "/dhs/config.yaml") {
		t.Errorf("default configPath = %q, want .../dhs/config.yaml", got)
	}
}

func TestConfigPath_DefaultUnresolvable(t *testing.T) {
	flagConfig = ""
	for _, k := range []string{"AppData", "XDG_CONFIG_HOME", "HOME"} {
		old, had := os.LookupEnv(k)
		os.Setenv(k, "")
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			}
		})
	}
	if _, err := configPath(); err == nil {
		t.Fatal("expected error when user config dir cannot be resolved")
	}
}

func TestLoadConfig_FromFlagPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("proxy: \"http://flag:1\"\nno_color: true\n"), 0644)
	flagConfig = path
	t.Cleanup(func() { flagConfig = "" })

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error: %v", err)
	}
	if cfg.Proxy != "http://flag:1" || !cfg.NoColor {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestLoadConfig_BadYAMLPropagates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(path, []byte("proxy: [unclosed"), 0644)
	flagConfig = path
	t.Cleanup(func() { flagConfig = "" })

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected yaml error from loadConfig")
	}
}

func TestLoadConfig_MissingFileIsZero(t *testing.T) {
	flagConfig = filepath.Join(t.TempDir(), "absent.yaml")
	t.Cleanup(func() { flagConfig = "" })
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("missing config must not error: %v", err)
	}
	if cfg.Proxy != "" || cfg.NoColor {
		t.Errorf("cfg = %+v, want zero", cfg)
	}
}

func TestLoadConfig_UnresolvableDefaultPathErrors(t *testing.T) {
	flagConfig = ""
	for _, k := range []string{"AppData", "XDG_CONFIG_HOME", "HOME"} {
		old, had := os.LookupEnv(k)
		os.Setenv(k, "")
		t.Cleanup(func() {
			if had {
				os.Setenv(k, old)
			}
		})
	}
	_, err := loadConfig()
	if err == nil || !strings.Contains(err.Error(), "resolving default config path") {
		t.Errorf("err = %v, want resolving default config path failure", err)
	}
}

// --- applyColorSetting ---

func TestApplyColorSetting(t *testing.T) {
	// Baseline under `go test`: stdout is not a TTY => NoColor is true.
	ui.SetEnabled(false)
	t.Cleanup(func() { ui.SetEnabled(false) })

	applyColorSetting(&config.Config{}) // NoColor=false => enable color
	if ui.Green("x") == "x" {
		t.Error("NoColor=false should enable ANSI color output")
	}
	applyColorSetting(&config.Config{NoColor: true})
	if got := ui.Green("x"); got != "x" {
		t.Errorf("NoColor=true should disable color, got %q", got)
	}
}

// --- errWithProxy ---

func TestErrWithProxy_EmptyReturnsSameError(t *testing.T) {
	sentinel := errors.New("boom")
	if got := errWithProxy(sentinel, ""); got != sentinel {
		t.Errorf("empty proxy must return the original error, got %v", got)
	}
}

func TestErrWithProxy_NonEmptyWraps(t *testing.T) {
	sentinel := errors.New("dial timeout")
	got := errWithProxy(sentinel, "http://192.168.0.225:7897")
	if !errors.Is(got, sentinel) {
		t.Error("wrapped error must still match the original via errors.Is")
	}
	if !strings.Contains(got.Error(), "http://192.168.0.225:7897") ||
		!strings.Contains(got.Error(), "当前代理") {
		t.Errorf("wrapped error should mention the active proxy, got %v", got)
	}
}

// --- shortDate ---

func TestShortDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-08-01T08:00:00Z", "2026-08-01"},
		{"2026-08-01T08:00:00.123456Z", "2026-08-01"},
		{"2026-08-01", "2026-08-01"}, // no T: unchanged
		{"", ""},
		{"Tonly", "Tonly"}, // T at index 0: i>0 false, unchanged
		{"abcTdef", "abc"},
	}
	for _, c := range cases {
		if got := shortDate(c.in); got != c.want {
			t.Errorf("shortDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
