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
	cyan = color.New(color.FgCyan)
	green = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	red = color.New(color.FgRed)
	bold = color.New(color.Bold)
}

// SetEnabled disables colored output globally when enabled is false
// (e.g. for --no-color, config no_color, or NO_COLOR).
//
// It deliberately never re-enables color when enabled is true: fatih/color
// already sets NoColor at init time from the no-color.org convention
// (non-empty NO_COLOR env) and non-TTY stdout detection. Forcing color on
// would clobber both, leaking ANSI escapes into pipes and ignoring NO_COLOR.
func SetEnabled(enabled bool) {
	if !enabled {
		color.NoColor = true
	}
}

// Cyan, Green, Yellow, Red, Bold wrap strings with the named style.
func Cyan(s string) string   { return cyan.Sprint(s) }
func Green(s string) string  { return green.Sprint(s) }
func Yellow(s string) string { return yellow.Sprint(s) }
func Red(s string) string    { return red.Sprint(s) }
func Bold(s string) string   { return bold.Sprint(s) }
