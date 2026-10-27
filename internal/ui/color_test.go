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

func TestSetEnabled_TrueAddsColor(t *testing.T) {
	saveColor(t)
	SetEnabled(true)
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
			t.Errorf("%s should emit ANSI escape when enabled, got %q", name, got)
		}
	}
}

func TestSetEnabled_ToggleRestores(t *testing.T) {
	saveColor(t)
	SetEnabled(true)
	if color.NoColor != false {
		t.Error("SetEnabled(true) should clear NoColor")
	}
	SetEnabled(false)
	if color.NoColor != true {
		t.Error("SetEnabled(false) should set NoColor")
	}
}

func TestColorWrappers_EachColorUsesDistinctCode(t *testing.T) {
	saveColor(t)
	SetEnabled(true)
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
