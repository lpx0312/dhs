package cmd

import "testing"

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"hello", 5, "hello"},            // exactly at limit: untouched
		{"hello", 6, "hello"},            // under limit
		{"hello", 4, "hell…"},            // over limit: clipped + ellipsis
		{"hello", 0, "hello"},            // max<=0: no truncation
		{"hello", -3, "hello"},           // negative: no truncation
		{"", 5, ""},                      // empty input
		{"abc", 100, "abc"},              // huge limit
		{"你好世界", 2, "你好…"},               // rune-aware, not byte-aware
		{"你好", 2, "你好"},                  // exact rune count
		{"aé€b", 3, "aé€…"},              // multibyte mix
		{"\x00\x01\x02", 2, "\x00\x01…"}, // control runes still counted as runes
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

// 【绿·钉住现状】truncate 按 rune（码点）而非 grapheme cluster 截断：
// 截断点可能落在组合字符序列或 ZWJ emoji 内部，把一个用户感知字符拆散。
// 输入列给出 rune 边界视角，期望值即当前实现的真实输出。
func TestTruncate_CutsInsideGraphemeClusters_GreenPin(t *testing.T) {
	// "é" 分解式 = 'e' + U+0301 组合尖音符，共 2 个 rune、1 个 grapheme。
	// max=1 切在 'e' 与组合音符之间：é 被拆成孤立的 'e'。
	if got := truncate("e\u0301x", 1); got != "e…" {
		t.Errorf("truncate(e+U+0301+x, 1) = %q, want %q（截断点在 rune 边界，拆散组合字符）", got, "e…")
	}
	// max=2 恰好保住 "é" 整体，佐证上一切掉的是组合音符而非不可见字符。
	if got := truncate("e\u0301x", 2); got != "e\u0301…" {
		t.Errorf("truncate(e+U+0301+x, 2) = %q, want %q", got, "é…(分解式)")
	}

	// ZWJ 家庭 emoji：👨 + ZWJ + 👩 + ZWJ + 👧，5 个 rune、1 个 grapheme。
	// max=2 保留 👨 + 裸 ZWJ，输出悬挂零宽连接符。
	family := "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	want := "\U0001F468\u200D…"
	if got := truncate(family, 2); got != want {
		t.Errorf("truncate(家庭emoji, 2) = %q, want %q（保留 👨+裸ZWJ）", got, want)
	}
	// 旗帜 "🇺🇸" = 两个 regional indicator rune。max=1 留下孤立的半个旗帜。
	if got := truncate("\U0001F1FA\U0001F1F8", 1); got != "\U0001F1FA…" {
		t.Errorf("truncate(US旗, 1) = %q, want %q（孤立 regional indicator）", got, "\U0001F1FA…")
	}
}
