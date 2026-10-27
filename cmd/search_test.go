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
