package logger

import "errors"

// ErrInvalidFormat is returned when the format value is unknown.
var ErrInvalidFormat = errors.New("invalid format value")

// Format selects the output representation of log records.
type Format string

const (
	// FormatAuto picks console output for EnvLocal and JSON otherwise.
	// The zero value behaves the same.
	FormatAuto Format = "auto"
	// FormatJSON emits one JSON object per line, for production and log
	// aggregators.
	FormatJSON Format = "json"
	// FormatConsole emits human-readable colored lines, for local
	// development.
	FormatConsole Format = "console"
)

// NewFormat parses value and resolves FormatAuto against env.
func NewFormat(value string, env Env) (Format, error) {
	format := Format(value)
	if value == "" {
		format = FormatAuto
	}

	switch format {
	case FormatAuto, FormatJSON, FormatConsole:
	default:
		return "", ErrInvalidFormat
	}

	if format == FormatAuto {
		if env == EnvLocal {
			format = FormatConsole
		} else {
			format = FormatJSON
		}
	}

	return format, nil
}

// String returns the format as a string.
func (f Format) String() string {
	return string(f)
}
