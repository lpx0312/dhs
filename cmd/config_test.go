package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dhs/internal/config"
	"dhs/internal/ui"
)

func TestNormalizeKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"proxy", "proxy", true},
		{"no-color", "no-color", true},
		{"no_color", "no-color", true},
		{"bogus", "", false},
	}
	for _, c := range cases {
		got, err := normalizeKey(c.in)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("normalizeKey(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("normalizeKey(%q) expected error, got %q", c.in, got)
		}
	}
}

func TestApplySetting_Proxy(t *testing.T) {
	orig := &config.Config{NoColor: true}
	next, err := applySetting(orig, "proxy", "http://p:7897")
	if err != nil {
		t.Fatalf("applySetting failed: %v", err)
	}
	if next.Proxy != "http://p:7897" {
		t.Errorf("proxy = %q, want http://p:7897", next.Proxy)
	}
	if next.NoColor != true || orig.Proxy != "" {
		t.Error("applySetting must not mutate the original config")
	}
}

func TestApplySetting_ProxyValidation(t *testing.T) {
	for _, bad := range []string{"", "192.168.0.225:7897", "http://"} {
		if _, err := applySetting(&config.Config{}, "proxy", bad); err == nil {
			t.Errorf("proxy %q should be rejected", bad)
		}
	}
}

func TestApplySetting_NoColor(t *testing.T) {
	next, err := applySetting(&config.Config{}, "no-color", "true")
	if err != nil {
		t.Fatalf("applySetting failed: %v", err)
	}
	if !next.NoColor {
		t.Error("no-color=true should set NoColor")
	}
	if _, err := applySetting(&config.Config{}, "no-color", "maybe"); err == nil {
		t.Error("no-color=maybe should be rejected")
	}
	if _, err := applySetting(&config.Config{}, "bogus", "x"); err == nil || !strings.Contains(err.Error(), "未知的配置项") {
		t.Errorf("unknown key should error, got %v", err)
	}
}

func TestClearSetting(t *testing.T) {
	orig := &config.Config{Proxy: "http://p:1", NoColor: true}
	next := clearSetting(orig, "proxy")
	if next.Proxy != "" || !next.NoColor {
		t.Errorf("clearSetting(proxy) = %+v, want only proxy cleared", next)
	}
	if orig.Proxy == "" {
		t.Error("clearSetting must not mutate the original config")
	}

	next = clearSetting(orig, "no-color")
	if next.NoColor || next.Proxy != "http://p:1" {
		t.Errorf("clearSetting(no-color) = %+v, want only no-color cleared", next)
	}
}

// 【绿·钉住现状】normalizeKey 不做大小写归一、不去除首尾空白：
// "Proxy"/" proxy"/"no-color " 等变体一律报"未知的配置项"。
// 若未来决定做归一化（strings.ToLower + TrimSpace），本测试会变红提醒同步更新文案。
func TestNormalizeKey_CaseAndWhitespaceVariantsRejected_GreenPin(t *testing.T) {
	for _, in := range []string{
		"Proxy", "PROXY", " proxy", "proxy ",
		"No-Color", "no-color ", " no_color", "no_color\t",
	} {
		got, err := normalizeKey(in)
		if err == nil {
			t.Errorf("normalizeKey(%q) = %q, nil; 当前不归一化大小写/空白，应报未知键", in, got)
		} else if !strings.Contains(err.Error(), "未知的配置项") {
			t.Errorf("normalizeKey(%q) err = %v, want 未知的配置项", in, err)
		}
	}
}

// 【绿·钉住现状】applySetting proxy 只要求 scheme 与 host 非空，没有 scheme 白名单：
// gopher://x、file://x 等非代理协议当前全部通过校验并原样写入。
// 对照组：scheme 有但 host 为空（即使带 path，如 http:///path）仍被拒绝。
func TestApplySetting_ProxySchemeNotWhitelisted_GreenPin(t *testing.T) {
	for _, v := range []string{"gopher://x", "file://x", "ftp://h:21"} {
		next, err := applySetting(&config.Config{}, "proxy", v)
		if err != nil {
			t.Errorf("applySetting(proxy, %q) err = %v; 当前无 scheme 白名单，应放行（钉住现状）", v, err)
			continue
		}
		if next.Proxy != v {
			t.Errorf("applySetting(proxy, %q).Proxy = %q, want 原样写入", v, next.Proxy)
		}
	}
	// 空 host（带 path 也算空 host）仍被现有校验拦截。
	for _, v := range []string{"http:///path", "file:///etc/passwd"} {
		if _, err := applySetting(&config.Config{}, "proxy", v); err == nil {
			t.Errorf("applySetting(proxy, %q) 应因 host 为空被拒绝", v)
		}
	}
}

// 【绿·钉住现状】no-color 错误文案声称"只接受 true/false"，但底层是
// strconv.ParseBool，实际宽容接受 "1"/"t"/"TRUE"/"0"/"f"/"FALSE" 等全集。
// 本测试钉住"宽容集成功"与错误文案之间的矛盾现状。
func TestApplySetting_NoColorParseBoolLenientSet_GreenPin(t *testing.T) {
	for _, v := range []string{"1", "t", "TRUE", "True"} {
		next, err := applySetting(&config.Config{}, "no-color", v)
		if err != nil || !next.NoColor {
			t.Errorf("applySetting(no-color, %q) = (NoColor=%v, err=%v); ParseBool 宽容集当前被接受，与文案\"只接受 true/false\"矛盾", v, next.NoColor, err)
		}
	}
	for _, v := range []string{"0", "f", "FALSE"} {
		next, err := applySetting(&config.Config{NoColor: true}, "no-color", v)
		if err != nil || next.NoColor {
			t.Errorf("applySetting(no-color, %q) = (NoColor=%v, err=%v); 应按 ParseBool 语义置 false", v, next.NoColor, err)
		}
	}
}

// --- config 四个子命令 RunE 直调（不经 cobra flag 解析） ---

// useTempConfigFile 将 flagConfig 指向临时目录里的新配置路径（不走 flag 解析），
// 测试结束后恢复原值。返回该路径。
func useTempConfigFile(t *testing.T) string {
	t.Helper()
	old := flagConfig
	path := filepath.Join(t.TempDir(), "config.yaml")
	flagConfig = path
	t.Cleanup(func() { flagConfig = old })
	return path
}

// captureRunEOutput 捕获 fn 执行期间写到 stdout 的全部内容
// （config 子命令的 RunE 直接用 fmt.Print* 输出）。
func captureRunEOutput(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()

	fn()

	w.Close()
	return <-done
}

// RunE 体内的 applyColorSetting 会翻转全局色彩开关，统一在 cleanup 复位为
// 非 TTY 默认（关闭），与 e2e 的 runCLI 行为一致。
func resetColorAfterRunE(t *testing.T) {
	t.Cleanup(func() { ui.SetEnabled(false) })
}

// set RunE 写入 → get RunE 裸值回读（含文件落盘双重验证）。
func TestConfigSetGetRunE_RoundTrip(t *testing.T) {
	path := useTempConfigFile(t)
	resetColorAfterRunE(t)

	if err := configSetCmd.RunE(nil, []string{"proxy", "http://p:1"}); err != nil {
		t.Fatalf("config set proxy RunE: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.Proxy != "http://p:1" {
		t.Errorf("文件回读 proxy = %q, want http://p:1", cfg.Proxy)
	}

	out := captureRunEOutput(t, func() {
		if err := configGetCmd.RunE(nil, []string{"proxy"}); err != nil {
			t.Errorf("config get proxy RunE: %v", err)
		}
	})
	if got := strings.TrimSpace(out); got != "http://p:1" {
		t.Errorf("get proxy 输出 = %q, want 裸值 http://p:1", got)
	}

	if err := configSetCmd.RunE(nil, []string{"no-color", "true"}); err != nil {
		t.Fatalf("config set no-color RunE: %v", err)
	}
	out = captureRunEOutput(t, func() {
		if err := configGetCmd.RunE(nil, []string{"no-color"}); err != nil {
			t.Errorf("config get no-color RunE: %v", err)
		}
	})
	if got := strings.TrimSpace(out); got != "true" {
		t.Errorf("get no-color 输出 = %q, want true", got)
	}
}

// get RunE 对未设置键输出占位符 "(未设置)"。
func TestConfigGetRunE_UnsetKeyPrintsPlaceholder(t *testing.T) {
	useTempConfigFile(t)
	resetColorAfterRunE(t)

	out := captureRunEOutput(t, func() {
		if err := configGetCmd.RunE(nil, []string{"proxy"}); err != nil {
			t.Errorf("get RunE: %v", err)
		}
	})
	if !strings.Contains(out, "(未设置)") {
		t.Errorf("空配置 get proxy 输出 = %q, want (未设置)", out)
	}
}

// unset RunE 清除目标键、保留其余键。
func TestConfigUnsetRunE_RestoresDefault(t *testing.T) {
	path := useTempConfigFile(t)
	resetColorAfterRunE(t)

	if err := config.Save(path, &config.Config{Proxy: "http://p:1", NoColor: true}); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	if err := configUnsetCmd.RunE(nil, []string{"proxy"}); err != nil {
		t.Fatalf("unset RunE: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if cfg.Proxy != "" {
		t.Errorf("unset 后 proxy = %q, want 已清除", cfg.Proxy)
	}
	if !cfg.NoColor {
		t.Error("unset proxy 不得连带清除 no-color")
	}
}

// set RunE 校验失败（值非法/键未知）必须发生在 Save 之前：
// 现有文件一个字节都不能变，不存在的文件不得被创建。
func TestConfigSetRunE_ValidationFailureLeavesFileUntouched(t *testing.T) {
	path := useTempConfigFile(t)
	resetColorAfterRunE(t)

	seed := "proxy: \"http://orig:1\"\nno_color: true\n"
	if err := os.WriteFile(path, []byte(seed), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 值校验失败（无 scheme）。
	if err := configSetCmd.RunE(nil, []string{"proxy", "not-a-url"}); err == nil {
		t.Fatal("非法 proxy 值应返回错误")
	}
	data, _ := os.ReadFile(path)
	if string(data) != seed {
		t.Errorf("值校验失败后文件被改动:\n got %q\nwant %q", data, seed)
	}

	// 键校验失败。
	if err := configSetCmd.RunE(nil, []string{"bogus", "x"}); err == nil {
		t.Fatal("未知键应返回错误")
	}
	data, _ = os.ReadFile(path)
	if string(data) != seed {
		t.Errorf("未知键失败后文件被改动:\n got %q\nwant %q", data, seed)
	}

	// 文件本不存在时，校验失败不得创建它。
	absent := filepath.Join(t.TempDir(), "absent.yaml")
	oldCfg := flagConfig
	flagConfig = absent
	t.Cleanup(func() { flagConfig = oldCfg })
	if err := configSetCmd.RunE(nil, []string{"proxy", "bad"}); err == nil {
		t.Fatal("非法值应返回错误")
	}
	if _, statErr := os.Stat(absent); !os.IsNotExist(statErr) {
		t.Errorf("校验失败不应创建配置文件: stat err = %v", statErr)
	}
}

// list RunE 输出配置文件路径与各键文件值。
func TestConfigListRunE_ShowsPathAndValues(t *testing.T) {
	path := useTempConfigFile(t)
	resetColorAfterRunE(t)
	oldProxy, oldNoColor := flagProxy, flagNoColor
	flagProxy, flagNoColor = "", false
	t.Cleanup(func() { flagProxy, flagNoColor = oldProxy, oldNoColor })

	if err := config.Save(path, &config.Config{Proxy: "http://p:1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	out := captureRunEOutput(t, func() {
		if err := configListCmd.RunE(nil, nil); err != nil {
			t.Errorf("list RunE: %v", err)
		}
	})
	for _, want := range []string{
		path,
		"文件值: http://p:1",
		"文件值: false",
		"生效代理: http://p:1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list 输出缺少 %q:\n%s", want, out)
		}
	}
}
