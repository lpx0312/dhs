package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/config"
	"dhs/internal/ui"

	"github.com/fatih/color"
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

// TestApplyColorSetting 【绿·钉修复语义】
// 修复前 applyColorSetting 无条件调用 ui.SetEnabled(!EffectiveNoColor)，
// SetEnabled(true) 会强行覆写 color.NoColor=false，破坏 fatih/color 的
// NO_COLOR / 非 TTY 自动检测（管道输出仍带 ANSI、NO_COLOR=1 被无视）。
// 修复后语义：只在需要禁色时调用 SetEnabled(false)，从不强制开色。
func TestApplyColorSetting(t *testing.T) {
	prev := color.NoColor
	t.Cleanup(func() {
		color.NoColor = prev
		flagNoColor = false
	})

	t.Run("no_color disables color", func(t *testing.T) {
		flagNoColor = false
		applyColorSetting(&config.Config{NoColor: true}) // from config file
		if got := ui.Green("x"); got != "x" {
			t.Errorf("config no_color=true should disable color, got %q", got)
		}
		flagNoColor = true
		applyColorSetting(&config.Config{}) // from --no-color flag
		if got := ui.Green("x"); got != "x" {
			t.Errorf("--no-color should disable color, got %q", got)
		}
		flagNoColor = false
	})

	t.Run("no source set does not override auto-detection", func(t *testing.T) {
		t.Setenv("NO_COLOR", "") // keep the env layer out of this check
		// Under `go test` stdout is not a TTY, so colored output cannot be
		// asserted here. Instead pin that applyColorSetting leaves fatih/color's
		// own NO_COLOR / TTY detection untouched for both possible baselines.
		for _, detected := range []bool{true, false} {
			color.NoColor = detected
			applyColorSetting(&config.Config{})
			if color.NoColor != detected {
				t.Errorf("no no-color source set: NoColor changed %v -> %v (must not override fatih/color auto-detection)",
					detected, color.NoColor)
			}
		}
	})

	t.Run("NO_COLOR env disables color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		color.NoColor = false // even if color had been force-enabled before
		flagNoColor = false
		applyColorSetting(&config.Config{})
		if got := ui.Green("x"); got != "x" {
			t.Errorf("NO_COLOR=1 should disable color, got %q", got)
		}
	})
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
	// This URL carries no userinfo, so url.URL.Redacted prints it unchanged;
	// credentials-bearing forms are pinned separately in
	// TestErrWithProxy_UserInfoRedacted.
	if !strings.Contains(got.Error(), "http://192.168.0.225:7897") ||
		!strings.Contains(got.Error(), "当前代理") {
		t.Errorf("wrapped error should mention the active proxy, got %v", got)
	}
}

// TestErrWithProxy_UserInfoRedacted 【绿·钉修复】：
// 代理 URL 含 userinfo（http://user:pass@host）时，errWithProxy 打印前必须
// 经 url.URL.Redacted 脱敏——密码以 xxxxx 形态出现，明文密码不得进入错误文本
// （错误会打到 stderr/日志）。username-only 形态（http://user@host，无密码）
// Redacted 原样保留。
func TestErrWithProxy_UserInfoRedacted(t *testing.T) {
	got := errWithProxy(errors.New("dial timeout"), "http://user:secret@192.168.0.1:7897")
	msg := got.Error()
	if !strings.Contains(msg, "user:xxxxx@192.168.0.1:7897") {
		t.Errorf("err = %q, want redacted form user:xxxxx@192.168.0.1:7897", msg)
	}
	if strings.Contains(msg, "secret") {
		t.Errorf("err = %q, password must not appear in the error text", msg)
	}

	// Username-only userinfo (no password) prints unchanged: Redacted only
	// masks passwords.
	got = errWithProxy(errors.New("dial timeout"), "http://user@192.168.0.1:7897")
	if !strings.Contains(got.Error(), "http://user@192.168.0.1:7897") {
		t.Errorf("err = %q, username-only userinfo should print unchanged", got.Error())
	}
}

// 【绿·钉住现状】errWithProxy 对 nil 错误不短路：proxy 非空时会用 %w 包装 nil，
// 得到一个非 nil、且正文带 "%!w(<nil>)" 的错误。修复方向（nil 直接透传）未定，
// 本测试钉住当前畸形行为，防止静默变化。
func TestErrWithProxy_NilErrorWithProxySet_GreenPin(t *testing.T) {
	got := errWithProxy(nil, "http://p:7897")
	if got == nil {
		t.Fatal("当前实现返回非 nil 错误（%w 包装 nil），got nil —— 行为已变化，请复核测试注释")
	}
	if !strings.Contains(got.Error(), "%!w(<nil>)") {
		t.Errorf("err = %q, want 正文包含 %%!w(<nil>)", got.Error())
	}
	if !strings.Contains(got.Error(), "http://p:7897") {
		t.Errorf("err = %q, want 仍附带代理地址", got.Error())
	}
	if errors.Is(got, nil) { // 编译期语义：非 nil 错误永远不匹配 nil 目标，仅作防御断言
		t.Error("非 nil 包装错误不应 errors.Is 匹配 nil")
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
