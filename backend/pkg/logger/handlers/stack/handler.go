package stack

import (
	"context"
	"log/slog"
	"runtime/debug"
)

// attrStackTrace is the log attribute name for the captured stack trace.
const attrStackTrace = "stack_trace"

// Handler attaches a "stack_trace" attribute to records logged at or above the
// threshold. The stack is captured at the log site — Handle is called
// synchronously from the caller's goroutine — so it shows where the record was
// emitted.
type Handler struct {
	next      slog.Handler
	threshold slog.Level
}

// NewHandler wraps next with a handler attaching stack traces at or above
// threshold.
func NewHandler(threshold slog.Level, next slog.Handler) *Handler {
	return &Handler{next: next, threshold: threshold}
}

// Enabled reports whether the next handler enables the level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// WithAttrs returns a new Handler whose attributes consist of both the
// receiver's attributes and the arguments.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), threshold: h.threshold}
}

// WithGroup returns a new Handler with the given group appended to the
// receiver's existing groups. The keys of all subsequent attributes, whether
// added by With or in a Record, should be qualified by the sequence of group
// names.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), threshold: h.threshold}
}

// Handle attaches the captured stack trace when the record reaches the
// threshold.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level < h.threshold {
		return h.next.Handle(ctx, rec)
	}

	withStack := rec.Clone()
	withStack.AddAttrs(slog.String(attrStackTrace, string(debug.Stack())))

	return h.next.Handle(ctx, withStack)
}
