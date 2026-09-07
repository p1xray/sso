package sanitize

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// redacted replaces every masked value.
const redacted = "[REDACTED]"

// defaultSanitizeKeys is the default deny-list for an OAuth 2.1 / OIDC
// provider: values of these keys are masked wherever the keys appear, at
// any group depth. Matching is exact and case-insensitive on the leaf
// key, so "code" masks a literal code attribute while status_code,
// error_code and friends survive.
var defaultSanitizeKeys = []string{
	"password", "passwd", "pwd",
	"secret", "client_secret", "private_key", "api_key", "apikey",
	"token", "access_token", "refresh_token", "id_token",
	"authorization", "proxy_authorization",
	"cookie", "set_cookie",
	"code", "authorization_code", "device_code",
	"nonce", "session_id",
}

// jwtRegex matches JWTs — access, refresh and ID tokens, and JWT-bearing
// assertions — anywhere inside a string value.
var jwtRegex = regexp.MustCompile(
	`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}(?:\.[A-Za-z0-9_-]+)?`)

// authSchemeRegex matches Authorization header values with the scheme
// preserved, so "Bearer dG9rZW4..." becomes "Bearer [REDACTED]".
var authSchemeRegex = regexp.MustCompile(
	`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)

// HandlerOptions are options for a sanitize handler. A zero HandlerOptions
// consists entirely of default values.
type HandlerOptions struct {
	// Disabled turns all masking off, including the value scan.
	Disabled bool
	// ExtraKeys adds key names to mask, in addition to the default
	// deny-list. Matching is exact and case-insensitive.
	ExtraKeys []string
	// ExceptKeys removes key names from the default deny-list, e.g.
	// "code" for a service that logs non-sensitive codes under it.
	ExceptKeys []string
	// DisableValueScan skips the JWT/Bearer/Basic scan of string values.
	DisableValueScan bool
}

// Handler masks sensitive data before the record reaches the
// underlying handler. It sits at the outer edge of the handler chain, so
// it sees call-site attributes and With-attached attributes; attributes
// appended by inner layers (request ID, stack) are trusted package code
// and are not scanned.
type Handler struct {
	opts *HandlerOptions
	next slog.Handler
	keys map[string]struct{}
}

// NewHandler wraps next with a sanitizer built from options.
func NewHandler(options *HandlerOptions, next slog.Handler) *Handler {
	if options == nil {
		options = &HandlerOptions{}
	}

	keys := make(map[string]struct{}, len(defaultSanitizeKeys)+len(options.ExtraKeys))
	for _, key := range defaultSanitizeKeys {
		keys[strings.ToLower(key)] = struct{}{}
	}

	for _, key := range options.ExceptKeys {
		delete(keys, strings.ToLower(key))
	}

	for _, key := range options.ExtraKeys {
		keys[strings.ToLower(key)] = struct{}{}
	}

	return &Handler{
		opts: options,
		next: next,
		keys: keys,
	}
}

// Enabled reports whether the next handler enables the level.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// WithAttrs sanitizes the attributes once, at attachment time, and keeps
// the sanitizer in the chain for the derived handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	sanitized := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		sanitized[i] = h.sanitizeAttr(attr)
	}

	return &Handler{
		opts: h.opts,
		next: h.next.WithAttrs(sanitized),
		keys: h.keys,
	}
}

// WithGroup returns a handler with the group applied to the next handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{
		opts: h.opts,
		next: h.next.WithGroup(name),
		keys: h.keys,
	}
}

// Handle rebuilds the record with sanitized attributes and passes it on.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if h.opts.Disabled {
		return h.next.Handle(ctx, rec)
	}

	sanitized := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(attr slog.Attr) bool {
		sanitized = append(sanitized, h.sanitizeAttr(attr))
		return true
	})

	clean := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	clean.AddAttrs(sanitized...)

	return h.next.Handle(ctx, clean)
}

func (h *Handler) sanitizeAttr(attr slog.Attr) slog.Attr {
	if attr.Equal(slog.Attr{}) {
		return attr
	}

	attr.Value = attr.Value.Resolve()
	if _, masked := h.keys[strings.ToLower(attr.Key)]; masked {
		return slog.String(attr.Key, redacted)
	}

	if attr.Value.Kind() == slog.KindGroup {
		children := attr.Value.Group()
		sanitized := make([]slog.Attr, len(children))
		for i, child := range children {
			sanitized[i] = h.sanitizeAttr(child)
		}

		return slog.Attr{Key: attr.Key, Value: slog.GroupValue(sanitized...)}
	}

	return slog.Attr{Key: attr.Key, Value: h.sanitizeValue(attr.Value)}
}

func (h *Handler) sanitizeValue(value slog.Value) slog.Value {
	if h.opts.DisableValueScan {
		return value
	}

	switch value.Kind() {
	case slog.KindString:
		return slog.StringValue(scanValue(value.String()))
	case slog.KindAny:
		if err, ok := value.Any().(error); ok {
			message := err.Error()
			if scanned := scanValue(message); scanned != message {
				return slog.StringValue(scanned)
			}
		}
	default:
		return value
	}

	return value
}

func scanValue(value string) string {
	jwtRedacted := jwtRegex.ReplaceAllString(value, redacted)
	authSchemeRedacted := authSchemeRegex.ReplaceAllString(jwtRedacted, "${1} "+redacted)
	return authSchemeRedacted
}
