package color

import (
	"fmt"
	"strconv"
)

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

type Colorizer func(colorCode int, value string) string

func WithColorize(colorCode int, value string) string {
	return fmt.Sprintf("\033[%sm%s%s", strconv.Itoa(colorCode), value, reset)
}

func WithoutColorize(_ int, value string) string {
	return value
}
