package sl

import (
	"log/slog"
	"strings"
)

// attrError is the log attribute name for errors.
const attrError = "error"

// Err returns an "error" attribute carrying the error's message.
func Err(err error) slog.Attr {
	return slog.Attr{
		Key:   attrError,
		Value: slog.StringValue(err.Error()),
	}
}

// Strings joins values with a single space into one string attribute,
// for short lists that read better on one line.
func Strings(key string, values []string) slog.Attr {
	return slog.Attr{
		Key:   key,
		Value: slog.StringValue(strings.Join(values, " ")),
	}
}
