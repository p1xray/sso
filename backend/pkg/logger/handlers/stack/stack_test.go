package stack

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const stackTraceDisabled = slog.Level(math.MaxInt32)

var testTime = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func newRecord(level slog.Level, message string, attrs ...slog.Attr) slog.Record {
	record := slog.NewRecord(testTime, level, message, 0)
	record.AddAttrs(attrs...)
	return record
}

func newTestHandler(buf *bytes.Buffer, threshold slog.Level) *Handler {
	sink := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewHandler(threshold, sink)
	return handler
}

func Test_Handle(t *testing.T) {
	tests := []struct {
		name               string
		threshold          slog.Level
		record             slog.Record
		stackTraceExpected bool
	}{
		{
			name:               "stack trace attribute is attached when threshold and record level are equals",
			threshold:          slog.LevelError,
			record:             newRecord(slog.LevelError, "msg"),
			stackTraceExpected: true,
		},
		{
			name:               "stack trace attribute is attached when threshold is lower then record level",
			threshold:          slog.LevelWarn,
			record:             newRecord(slog.LevelError, "msg"),
			stackTraceExpected: true,
		},
		{
			name:               "stack trace attribute is not attached when threshold is higher then record level",
			threshold:          slog.LevelError,
			record:             newRecord(slog.LevelInfo, "msg"),
			stackTraceExpected: false,
		},
		{
			name:               "stack trace attribute is not attached on stack trace disabled",
			threshold:          stackTraceDisabled,
			record:             newRecord(slog.LevelError, "msg"),
			stackTraceExpected: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			handler := newTestHandler(buf, test.threshold)

			err := handler.Handle(context.Background(), test.record)
			require.NoError(t, err)

			got := buf.String()
			stackTracePresent := strings.Contains(got, "stack_trace=")
			if test.stackTraceExpected {
				assert.True(t, stackTracePresent, "expected stack trace attribute to be attached, got %q", got)
			} else {
				assert.False(t, stackTracePresent, "expected no stack trace attribute to be attached, got %q", got)
			}
		})
	}
}

func Test_WithAttrs_KeepsThreshold(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := newTestHandler(buf, slog.LevelError)

	derived := handler.WithAttrs([]slog.Attr{slog.String("a", "abc")})

	err := derived.Handle(context.Background(), newRecord(slog.LevelError, "msg"))
	require.NoError(t, err)

	got := buf.String()
	if !strings.Contains(got, `stack_trace="`) || !strings.Contains(got, "a=abc") {
		t.Errorf("expected stack and additional attributes on derived logger, got: %s", got)
	}
}

func Test_WithGroup_KeepsThreshold(t *testing.T) {
	buf := &bytes.Buffer{}
	handler := newTestHandler(buf, slog.LevelError)

	derived := handler.WithGroup("abc")

	err := derived.Handle(context.Background(), newRecord(slog.LevelError, "msg"))
	require.NoError(t, err)

	got := buf.String()
	if !strings.Contains(got, `stack_trace="`) || !strings.Contains(got, "abc.") {
		t.Errorf("expected stack and group on derived logger, got: %s", got)
	}
}

func Test_Enabled_SuccessfulDelegates(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewHandler(slog.LevelError, sink)

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug must be disabled by the sink's info level")
	}
	if !handler.Enabled(context.Background(), slog.LevelError) {
		t.Error("error must be enabled")
	}
}
