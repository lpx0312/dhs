package cmd

import (
	"strings"
	"testing"

	"dhs/internal/config"
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
}
