package pretty

import (
	"log/slog"
	"time"
)

var testTime = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func newRecord(level slog.Level, message string, attrs ...slog.Attr) slog.Record {
	record := slog.NewRecord(testTime, level, message, 0)
	record.AddAttrs(attrs...)
	return record
}
