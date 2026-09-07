package logger

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Level is a log level as a string.
type Level string

// Named log levels.
const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// NewLevel returns the level from value, defaulting by env: debug for local and
// dev, info for prod.
func NewLevel(value string, env Env) Level {
	if value == "" {
		if env != EnvProd {
			return LevelDebug
		}

		return LevelInfo
	}

	return Level(value)
}

// String returns the level as a string.
func (l Level) String() string {
	return string(l)
}

// ParseToSlogLevel converts the level to a slog.Level.
func (l Level) ParseToSlogLevel() (slog.Level, error) {
	base, offset, err := l.parseComposite()
	if err != nil {
		return 0, fmt.Errorf("parse: %w", err)
	}

	level, err := l.parseBase(base)
	if err != nil {
		return 0, fmt.Errorf("parse: %w", err)
	}

	return level + slog.Level(offset), nil
}

func (l Level) parseComposite() (string, int, error) {
	base, offset := l.String(), 0
	if !l.isComposite() {
		return base, offset, nil
	}

	separatorIndex := strings.IndexAny(base, "+-")
	offset, err := strconv.Atoi(base[separatorIndex:])
	if err != nil {
		return "", 0, fmt.Errorf("invalid level: %s", base)
	}

	base = base[:separatorIndex]
	return base, offset, nil
}

func (l Level) isComposite() bool {
	return strings.ContainsAny(l.String(), "+-")
}

func (l Level) parseBase(base string) (slog.Level, error) {
	var level slog.Level
	switch strings.ToLower(base) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		// Not a name — maybe a plain number on the slog level scale.
		return l.parsePlain(base)
	}

	return level, nil
}

func (l Level) parsePlain(plain string) (slog.Level, error) {
	number, err := strconv.Atoi(plain)
	if err != nil {
		return 0, fmt.Errorf("invalid level %s", plain)
	}

	return slog.Level(number), nil
}
