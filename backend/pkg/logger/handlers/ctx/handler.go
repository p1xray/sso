package ctx

import (
	"context"
	"log/slog"
)

// ContextAttrFunc extracts log attributes from the logging context.
type ContextAttrFunc func(context.Context) []slog.Attr

// Handler injects attributes extracted from the logging context.
type Handler struct {
	next       slog.Handler
	extractors []ContextAttrFunc
}

// NewHandler wraps next with a handler injecting attrs extracted by extractors.
func NewHandler(extractors []ContextAttrFunc, next slog.Handler) *Handler {
	return &Handler{next: next, extractors: extractors}
}

// Enabled reports whether the next handler enables the level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// WithAttrs returns a handler with the attributes applied to the next handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs), extractors: h.extractors}
}

// WithGroup returns a handler with the group applied to the next handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), extractors: h.extractors}
}

// Handle prepends attrs extracted from the logging context to the record.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	var extracted []slog.Attr
	for _, extract := range h.extractors {
		extracted = append(extracted, extract(ctx)...)
	}

	if len(extracted) < 1 {
		return h.next.Handle(ctx, rec)
	}

	enriched := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	enriched.AddAttrs(extracted...)
	rec.Attrs(func(attr slog.Attr) bool {
		enriched.AddAttrs(attr)
		return true
	})

	return h.next.Handle(ctx, enriched)
}
