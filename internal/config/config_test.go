package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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
	// t.Setenv restores the pre-test value (or unsets it) automatically.
	t.Setenv("HTTPS_PROXY", "http://from-env:9999")
	got := cfg.EffectiveProxy("", "")
	if got != "http://from-env:9999" {
		t.Errorf("EffectiveProxy = %q, want env value", got)
	}
}

func TestEffectiveProxy_EnvHTTPSFallback(t *testing.T) {
	cfg := &Config{}
	// Empty HTTPS_PROXY is equivalent to unset for EffectiveProxy (only
	// non-empty values apply); setting it explicitly keeps the test
	// deterministic even when the dev shell exports a real proxy, and
	// t.Setenv restores the original value afterwards.
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("HTTP_PROXY", "http://from-http-env:1")
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

// --- 追加：审查结论钉住测试（标注【绿·钉住现状】/【绿·钉修复】） ---

// setOrClearEnv sets key to value when set is true, unsets it when set is
// false, and restores the original state when the test finishes.
func setOrClearEnv(t *testing.T, key, value string, set bool) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if set {
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Setenv %s: %v", key, err)
		}
	} else {
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("Unsetenv %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

// TestEffectiveNoColor_HonorsNO_COLOREnv 【绿·钉修复】审查 P1#1：
// EffectiveNoColor 已补 NO_COLOR env 层级。按 no-color.org 约定，
// NO_COLOR 为任意非空值时应禁用颜色。修复前实现完全不看该变量，
// 本测试钉 bug 为红；补 env 层后转绿（优先级 flag > file > NO_COLOR env）。
func TestEffectiveNoColor_HonorsNO_COLOREnv(t *testing.T) {
	setOrClearEnv(t, "NO_COLOR", "1", true)
	cfg := &Config{} // file no_color=false
	if got := cfg.EffectiveNoColor(false); !got {
		t.Errorf("EffectiveNoColor(false) = false with NO_COLOR=1, want true (no-color.org: any non-empty NO_COLOR disables color)")
	}
}

// TestEffectiveNoColor_PriorityFlagFileEnvTable 【绿·钉修复】审查 P1#1：
// flag × 文件 × env 的 8 组合优先级表。期望优先级 flag > file > NO_COLOR。
// 修复前实现缺 env 层，仅 env_only 一行（flag=false, file=false, env=1）失败；
// 补 env 层后整表全绿。
func TestEffectiveNoColor_PriorityFlagFileEnvTable(t *testing.T) {
	cases := []struct {
		name   string
		flag   bool
		file   bool
		envSet bool // NO_COLOR 设为 "1" 还是清除
		want   bool
	}{
		{"none_set", false, false, false, false},
		{"flag_only", true, false, false, true},
		{"file_only", false, true, false, true},
		{"env_only", false, false, true, true}, // 修复前返回 false，补 env 层后绿
		{"flag_beats_file", true, true, false, true},
		{"flag_beats_env", true, false, true, true},
		{"file_beats_env", false, true, true, true},
		{"all_set_flag_wins", true, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setOrClearEnv(t, "NO_COLOR", "1", tc.envSet)
			cfg := &Config{NoColor: tc.file}
			if got := cfg.EffectiveNoColor(tc.flag); got != tc.want {
				t.Errorf("flag=%v file=%v envSet=%v: EffectiveNoColor = %v, want %v (priority flag > file > NO_COLOR)",
					tc.flag, tc.file, tc.envSet, got, tc.want)
			}
		})
	}
}

// TestEffectiveNoColor_EmptyNO_COLOREnvIgnored 【绿·钉语义】：
// 钉住 NO_COLOR=""（空值）不应视为已设置——no-color.org 约定为"非空即生效"。
// 实现已读该变量（config.go EffectiveNoColor 的 env 层），只是空值不生效；
// 若未来调整 env 层，必须继续只认非空值。
func TestEffectiveNoColor_EmptyNO_COLOREnvIgnored(t *testing.T) {
	setOrClearEnv(t, "NO_COLOR", "", true)
	cfg := &Config{}
	if got := cfg.EffectiveNoColor(false); got {
		t.Errorf("EffectiveNoColor = true with NO_COLOR=\"\", want false (empty value must not count per no-color.org)")
	}
}

// TestSave_OverwriteExistingFileTruncatesAndReloads 【绿·钉住现状】审查 P1#2：
// Save 非原子（直接 WriteFile 截断写，无 temp+rename），并发 Load 可能读到
// 截断/空文件并静默得到零值——该竞态难以稳定复现，此处改钉两点可观察行为：
// (a) 覆盖已存在的更长旧文件后内容完整、无旧字节残留（O_TRUNC 生效）；
// (b) 覆盖后的文件能原样 Load 回来。原子性本身（temp+rename）仍待修复。
func TestSave_OverwriteExistingFileTruncatesAndReloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	stale := "proxy: http://stale-very-long-old-value.example.com:1234\n" +
		"no_color: true\n" +
		"# STALE-MARKER-SHOULD-NOT-SURVIVE padding padding padding padding padding\n"
	if err := os.WriteFile(path, []byte(stale), 0644); err != nil {
		t.Fatal(err)
	}
	fresh := &Config{Proxy: "http://fresh:1", NoColor: false}
	if err := Save(path, fresh); err != nil {
		t.Fatalf("Save over existing file: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "STALE-MARKER-SHOULD-NOT-SURVIVE") {
		t.Errorf("Save left stale bytes behind, file = %q", raw)
	}
	wantBytes, err := yaml.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(wantBytes) {
		t.Errorf("file content = %q, want exactly %q", raw, wantBytes)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after overwrite Save: %v", err)
	}
	if got.Proxy != fresh.Proxy || got.NoColor != fresh.NoColor {
		t.Errorf("reload = %+v, want %+v", got, fresh)
	}
}

// TestSave_DoesNotTightenPermissionsOnExistingFile 【绿·钉住现状】审查 P2#4：
// os.WriteFile 的权限位仅在创建时生效，覆盖已存在的 0644 文件后权限不会收紧
// 到 Save 声明的 0600（代理 URL 可能含凭据）。当前行为是保持 0644，本测试
// 钉住该现状；审查建议修复为写后 os.Chmod(path, 0600)（届时本测试需反转）。
// POSIX-only：Windows 无 POSIX 权限位。
func TestSave_DoesNotTightenPermissionsOnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("proxy: old\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, &Config{Proxy: "http://new:1"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Errorf("perm after Save over 0644 file = %o, want 0644 (current behavior: not tightened to 0600; see review P2#4)", got)
	}
}

// TestSave_NewFilePermissions0600 【绿·钉住现状】审查 P2#4 配套：
// 新建文件按 Save 声明取 0600（umask 只会清位，0600 仅含 owner 位，
// 常规 umask 022/077 下不受影响）。POSIX-only。
func TestSave_NewFilePermissions0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	path := filepath.Join(t.TempDir(), "dhs", "config.yaml")
	if err := Save(path, &Config{Proxy: "http://p:1"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("perm of new file = %o, want 0600", got)
	}
}

// TestLoad_ProxyWhitespacePreservedFromFile 【绿·钉住现状】审查 P2#3：
// 文件中 proxy 为仅空白串 / 带尾随空白时，Load 原样返回，不做 TrimSpace。
// 当前行为如此，本测试钉住；审查建议是加载时 TrimSpace（届时 want 需改为
// 修剪后的值）。
func TestLoad_ProxyWhitespacePreservedFromFile(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"whitespace_only", "proxy: \"  \"\n", "  "},
		{"trailing_space", "proxy: 'http://p:1 '\n", "http://p:1 "},
		{"trailing_cr", "proxy: \"http://p:1\\r\"\n", "http://p:1\r"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0644); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatalf("Load(%s): %v", tc.name, err)
			}
			if cfg.Proxy != tc.want {
				t.Errorf("Load %s: proxy = %q, want %q (current behavior: no trim)", tc.name, cfg.Proxy, tc.want)
			}
		})
	}
}

// TestEffectiveProxy_WhitespaceFileProxyShadowsEnv 【绿·钉住现状】审查 P2#3：
// 文件 proxy 为仅空白串时因"非空即生效"判断压过 env 层并原样返回，
// 不回退到 HTTPS_PROXY。钉住当前行为；审查建议 TrimSpace 后再判空。
func TestEffectiveProxy_WhitespaceFileProxyShadowsEnv(t *testing.T) {
	setOrClearEnv(t, "HTTPS_PROXY", "http://from-env:1", true)
	setOrClearEnv(t, "HTTP_PROXY", "http://from-env:2", true)
	cfg := &Config{Proxy: "  "}
	if got := cfg.EffectiveProxy("", ""); got != "  " {
		t.Errorf("EffectiveProxy = %q, want %q (current behavior: whitespace-only file proxy wins over env)", got, "  ")
	}
}

// TestLoad_UnknownFieldsIgnoredForwardCompat 【绿·钉住现状】审查 P2：
// 配置文件包含未知字段（如新版 dhs 写出的 extra/nested）时被静默忽略，
// 加载不报错、已知字段正常解析。钉住该前向兼容行为。
func TestLoad_UnknownFieldsIgnoredForwardCompat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "proxy: \"http://x:1\"\nextra_unknown: 42\nnested:\n  deep: true\nno_color: false\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load with unknown fields should not error, got %v", err)
	}
	if cfg.Proxy != "http://x:1" {
		t.Errorf("proxy = %q, want http://x:1", cfg.Proxy)
	}
	if cfg.NoColor {
		t.Error("no_color should be false")
	}
}

// TestEffectiveProxy_LowercaseEnvVarsPlatformBehavior 【绿·钉住现状】审查 P2#3：
// EffectiveProxy 只查大写 HTTPS_PROXY/HTTP_PROXY。Linux 上小写 https_proxy
// 不会命中（os.Getenv 大小写敏感），应返回 ""——本测试 POSIX 分支钉住该缺口。
// Windows 实测（win32, go1.26.4）：环境变量名大小写不敏感，
// 设置小写 https_proxy 后 os.Getenv("HTTPS_PROXY") 能读到同一值，
// EffectiveProxy 因此返回它——Windows 分支钉住这种"碰巧生效"。
func TestEffectiveProxy_LowercaseEnvVarsPlatformBehavior(t *testing.T) {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		setOrClearEnv(t, k, "", false)
	}
	setOrClearEnv(t, "https_proxy", "http://lower-set:1", true)
	cfg := &Config{}
	got := cfg.EffectiveProxy("", "")
	if runtime.GOOS == "windows" {
		if got != "http://lower-set:1" {
			t.Errorf("windows: EffectiveProxy = %q, want %q (env names are case-insensitive on Windows)", got, "http://lower-set:1")
		}
	} else if got != "" {
		t.Errorf("posix: EffectiveProxy = %q, want \"\" (lowercase https_proxy must not be read; only HTTPS_PROXY/HTTP_PROXY are consulted)", got)
	}
}

// TestLoad_InvalidYAMLErrorLacksFilePath 【绿·钉住现状】审查 P3：
// 坏 YAML 时返回的错误信息不包含文件路径，调用方日志里难以定位是哪个
// 配置文件坏了。钉住当前行为；审查建议 wrap 路径上下文
// （如 fmt.Errorf("parsing config %s: %w", path, err)，届时本测试需反转）。
func TestLoad_InvalidYAMLErrorLacksFilePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("proxy: [unclosed"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if strings.Contains(err.Error(), path) || strings.Contains(err.Error(), filepath.Base(path)) {
		t.Errorf("error unexpectedly contains the file path, want path-free error (current behavior): %v", err)
	}
}

// TestSave_RoundTripSpecialCharacters 【绿·钉住现状】：
// Proxy 含 URL fragment(#)、中文字符、@、query 等特殊字符时的
// YAML 序列化/反序列化往返一致性。
func TestSave_RoundTripSpecialCharacters(t *testing.T) {
	want := &Config{
		Proxy:   "http://user:p%40ss@proxy.example.com:8080#锚点-frag?x=1&lang=中文",
		NoColor: true,
	}
	path := filepath.Join(t.TempDir(), "dhs", "config.yaml")
	if err := Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got.Proxy != want.Proxy {
		t.Errorf("proxy round trip = %q, want %q", got.Proxy, want.Proxy)
	}
	if got.NoColor != want.NoColor {
		t.Errorf("no_color round trip = %v, want %v", got.NoColor, want.NoColor)
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
