package ui

import (
	"github.com/fatih/color"
)

var (
	cyan   *color.Color
	green  *color.Color
	yellow *color.Color
	red    *color.Color
	bold   *color.Color
)

func init() {
	// color.NoColor auto-detects non-TTY; SetEnabled applies an explicit override.
	cyan = color.New(color.FgCyan)
	green = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	red = color.New(color.FgRed)
	bold = color.New(color.Bold)
}

// SetEnabled turns colored output on/off globally. Pass false to disable
// (e.g. for --no-color or when stdout is not a TTY).
func SetEnabled(enabled bool) {
	color.NoColor = !enabled
}

// Cyan, Green, Yellow, Red, Bold wrap strings with the named style.
func Cyan(s string) string   { return cyan.Sprint(s) }
func Green(s string) string  { return green.Sprint(s) }
func Yellow(s string) string { return yellow.Sprint(s) }
func Red(s string) string    { return red.Sprint(s) }
func Bold(s string) string   { return bold.Sprint(s) }
