package color

import (
	"fmt"
	"strconv"
)

// ANSI color codes.
const (
	reset = "\033[0m"

	Cyan         = 36
	LightGray    = 37
	DarkGray     = 90
	LightRed     = 91
	LightYellow  = 93
	LightBlue    = 94
	LightMagenta = 95
	White        = 97
)

// Colorizer wraps a value in an ANSI color code or returns it unchanged.
type Colorizer func(colorCode int, value string) string

// WithColorize wraps value in the given ANSI color code.
func WithColorize(colorCode int, value string) string {
	return fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), value, reset)
}

// WithoutColorize returns value unchanged.
func WithoutColorize(_ int, value string) string {
	return value
}
