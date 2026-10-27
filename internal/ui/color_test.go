package ui

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

// saveColor restores the global color state after the test.
func saveColor(t *testing.T) {
	t.Helper()
	prev := color.NoColor
	t.Cleanup(func() { color.NoColor = prev })
}

func TestSetEnabled_FalseStripsColor(t *testing.T) {
	saveColor(t)
	SetEnabled(false)
	if color.NoColor != true {
		t.Fatalf("NoColor = %v, want true", color.NoColor)
	}
	if got := Green("ok"); got != "ok" {
		t.Errorf("Green with color disabled = %q, want plain %q", got, "ok")
	}
	if got := Cyan("x"); got != "x" {
		t.Errorf("Cyan with color disabled = %q, want plain", got)
	}
}

// TestSetEnabled_TruePreservesEnabledColor 【绿·钉修复语义】
// SetEnabled 是"只禁不开"语义：enabled=true 时不覆写 color.NoColor。
// 模拟彩色已开的场景（TTY 且无 NO_COLOR），显式置 color.NoColor=false
// 后调用 SetEnabled(true)，应保持彩色输出。
func TestSetEnabled_TruePreservesEnabledColor(t *testing.T) {
	saveColor(t)
	color.NoColor = false // simulate color-capable output
	SetEnabled(true)
	if color.NoColor != false {
		t.Fatalf("SetEnabled(true) 不应覆写 color.NoColor: got %v, want false", color.NoColor)
	}
	for name, fn := range map[string]func(string) string{
		"Cyan":   Cyan,
		"Green":  Green,
		"Yellow": Yellow,
		"Red":    Red,
		"Bold":   Bold,
	} {
		got := fn("word")
		if !strings.Contains(got, "word") {
			t.Errorf("%s lost the input text: %q", name, got)
		}
		if !strings.Contains(got, "\x1b[") {
			t.Errorf("%s should emit ANSI escape when color is on and SetEnabled(true) is called, got %q", name, got)
		}
	}
}

// TestSetEnabled_ToggleOnlyDisables 【绿·钉修复语义】
// SetEnabled(false) 禁色；SetEnabled(true) 不能反向解禁——
// NO_COLOR / 非 TTY 检测得出的 NoColor=true 必须保持。
func TestSetEnabled_ToggleOnlyDisables(t *testing.T) {
	saveColor(t)
	color.NoColor = false // color on
	SetEnabled(true)
	if color.NoColor != false {
		t.Error("SetEnabled(true) should keep color enabled")
	}
	SetEnabled(false)
	if color.NoColor != true {
		t.Error("SetEnabled(false) should set NoColor")
	}
	SetEnabled(true)
	if color.NoColor != true {
		t.Error("SetEnabled(true) must not clear NoColor (only-disable semantics)")
	}
}

// TestSetEnabled_TrueHonorsNOColorEnv 【绿·钉修复】
// 修复前 SetEnabled(true) 无条件覆写 color.NoColor，破坏 fatih/color 的
// NO_COLOR 环境变量约定（https://no-color.org/）：NO_COLOR=1 时即使调用方
// SetEnabled(true) 也不应输出 ANSI 码。修复后 SetEnabled 只禁不开，
// NO_COLOR=1 下包 init 得出的 NoColor=true 得以保留，本测试转绿。
func TestSetEnabled_TrueHonorsNOColorEnv(t *testing.T) {
	saveColor(t)
	t.Setenv("NO_COLOR", "1")
	// 模拟 fatih/color 在 NO_COLOR=1 下包 init 会得出的全局状态：
	// env 只在 New()/init 时读取，t.Setenv 不会回溯改变它，故手动置位。
	color.NoColor = true
	SetEnabled(true)
	if color.NoColor != true {
		t.Errorf("NO_COLOR=1 时 SetEnabled(true) 不应清除 NoColor: NoColor = %v", color.NoColor)
	}
	if got := Green("ok"); strings.Contains(got, "\x1b[") {
		t.Errorf("NO_COLOR=1 + SetEnabled(true) 后 Green 不应输出 ANSI 码, got %q", got)
	}
}

// TestSetEnabled_FalseRestoresPlainAfterEnable 【绿·钉住现状】
// SetEnabled(false) 在彩色开启后能恢复无色输出。
func TestSetEnabled_FalseRestoresPlainAfterEnable(t *testing.T) {
	saveColor(t)
	color.NoColor = false // color on (explicit; SetEnabled no longer force-enables)
	SetEnabled(true)
	if got := Green("x"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("前置条件失败: 彩色开启时 Green 应输出 ANSI 码, got %q", got)
	}
	SetEnabled(false)
	if got := Green("ok"); got != "ok" {
		t.Errorf("SetEnabled(false) 后 Green 应恢复纯文本, got %q", got)
	}
}

func TestColorWrappers_EachColorUsesDistinctCode(t *testing.T) {
	saveColor(t)
	color.NoColor = false // color on
	SetEnabled(true)      // must not turn it off
	pairs := []struct {
		name string
		got  string
		code string
	}{
		{"Cyan", Cyan("t"), "\x1b[36m"},
		{"Green", Green("t"), "\x1b[32m"},
		{"Yellow", Yellow("t"), "\x1b[33m"},
		{"Red", Red("t"), "\x1b[31m"},
	}
	for _, p := range pairs {
		if !strings.HasPrefix(p.got, p.code) {
			t.Errorf("%s = %q, want prefix %q", p.name, p.got, p.code)
		}
	}
}
