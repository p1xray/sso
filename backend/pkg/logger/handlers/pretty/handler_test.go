package pretty

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
)

// countingWriter counts Write calls so tests can verify that the handler
// emits the whole record with a single Write.
type countingWriter struct {
	buf    bytes.Buffer
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.writes++
	return w.buf.Write(p)
}

func handle(t *testing.T, handler *Handler, record slog.Record) string {
	t.Helper()
	writer := &bytes.Buffer{}
	handler.writer = writer
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	return writer.String()
}

func Test_Handle_SingleWritePerRecord(t *testing.T) {
	writer := &countingWriter{}
	handler := NewHandler(writer, &HandlerOptions{NoColor: true})

	record := newRecord(slog.LevelInfo, "msg", slog.Int("a", 1), slog.String("b", "x"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}

	if writer.writes != 1 {
		t.Errorf("expected 1 write per record, got %d", writer.writes)
	}
}

func Test_Handle_SuccessfulLevelFiltering(t *testing.T) {
	handler := NewHandler(&bytes.Buffer{}, &HandlerOptions{NoColor: true})

	if got := handle(t, handler, newRecord(slog.LevelDebug, "dbg")); got != "" {
		t.Errorf("expected debug record to be dropped at default info level, got %q", got)
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected warn to be enabled at default info level")
	}
	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be disabled at default info level")
	}
}

func Test_Handle_SuccessfulDynamicChangeLevel(t *testing.T) {
	levelVar := new(slog.LevelVar) // zero value is Info
	handler := NewHandler(&bytes.Buffer{}, &HandlerOptions{Level: levelVar, NoColor: true})

	if got := handle(t, handler, newRecord(slog.LevelDebug, "dbg")); got != "" {
		t.Errorf("expected debug record to be dropped, got %q", got)
	}

	levelVar.Set(slog.LevelDebug)
	if got := handle(t, handler, newRecord(slog.LevelDebug, "dbg")); got == "" {
		t.Error("expected debug record to pass after SetLevel to debug")
	}
}
