package sl

import (
	"log/slog"
	"strings"
)

func Err(err error) slog.Attr {
	return slog.Attr{
		Key:   "error",
		Value: slog.StringValue(err.Error()),
	}
}

func Strings(key string, values []string) slog.Attr {
	return slog.Attr{
		Key:   key,
		Value: slog.StringValue(strings.Join(values, " ")),
	}
}
