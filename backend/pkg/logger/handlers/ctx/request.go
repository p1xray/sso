package ctx

import (
	"context"
	"log/slog"
)

// attrRequestID is the log attribute name for the request ID.
const attrRequestID = "request_id"

// requestIDKey is a context key type for request ID.
type requestIDKey struct{}

// WithRequestID returns a copy of ctx carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request ID from ctx, or an empty string when
// there is none.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// ExtractRequestID is the default context extractor: it adds request_id when the
// context carries one.
func ExtractRequestID(ctx context.Context) []slog.Attr {
	id := RequestIDFromContext(ctx)
	if id == "" {
		return nil
	}
	return []slog.Attr{slog.String(attrRequestID, id)}
}
