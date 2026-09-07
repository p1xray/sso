// Package logger provides production logging on top of log/slog.
//
// Services consume the logger through the Logger interface. Every logging method
// takes the context.Context first, and that context is what carries the request
// ID into the record: there is no way to log without one.
//
// New builds the logger from an explicit Config - the package reads no
// environment variables and holds no package-level state, and it never calls
// slog.SetDefault. Services that want third-party libraries logging through the
// same pipeline may call slog.SetDefault(lg.Unwrap()) themselves in main.
//
// Every record carries the base attributes service, env and, when set, version
// and instance, plus request_id when the logging context carries one.
//
// Attribute keys are snake_case. Sensitive keys (passwords, tokens, client
// secrets, authorization codes, cookies and friends) are masked with [REDACTED]
// by default, see SanitizeConfig to adjust the deny-list or disable masking.
// Records at error level and above carry a "stack_trace" attribute captured at
// the log site, see Config.StackTraceLevel.
//
// Output goes to a single io.Writer (stdout by default) as strict JSON for dev
// and prod and as colored console lines for local, see Format. The level can
// change at runtime through SetLevel or LevelVar.
package logger

import (
	"context"
	"io"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/ctx"
	"github.com/p1xray/sso/backend/pkg/logger/handlers/pretty"
	"github.com/p1xray/sso/backend/pkg/logger/handlers/sanitize"
	"github.com/p1xray/sso/backend/pkg/logger/handlers/stack"

	"log/slog"
)

// Base attribute names carried by every record.
const (
	attrService  = "service"
	attrEnv      = "env"
	attrVersion  = "version"
	attrInstance = "instance"
)

// Logger is the logging contract for service code. All methods take the
// context first — the context carries the request ID into every record.
type Logger interface {
	// Debug logs the message at debug level.
	Debug(ctx context.Context, msg string, args ...any)

	// Info logs the message at info level.
	Info(ctx context.Context, msg string, args ...any)

	// Warn logs the message at warn level.
	Warn(ctx context.Context, msg string, args ...any)

	// Error logs the message at error level.
	Error(ctx context.Context, msg string, args ...any)

	// Enabled reports whether records at the level would be logged; use
	// it to guard the construction of expensive debug payloads.
	Enabled(ctx context.Context, level slog.Level) bool

	// With returns a logger whose records also carry the given
	// attributes, e.g. With("component", "storage").
	With(args ...any) Logger

	// WithGroup returns a logger whose attributes appear inside the
	// named group.
	WithGroup(name string) Logger

	// SetLevel changes the minimum level at runtime, for this logger and
	// every logger derived from it.
	SetLevel(level slog.Level)
}

type options struct {
	Service           string
	Env               Env
	Version           string
	Instance          string
	Format            Format
	Level             slog.Level
	Writer            io.Writer
	NoColor           bool
	AddSource         *bool
	StackTraceLevel   slog.Level
	Sanitize          SanitizeConfig
	ContextExtractors []ctx.ContextAttrFunc
}

type logger struct {
	inner *slog.Logger
	level *slog.LevelVar
}

// compile-time check that the concrete type satisfies the contract.
var _ Logger = (*logger)(nil)

// New builds a logger from cfg, applying environment defaults for unset
// fields (see Config). It fails fast on identity and configuration
// mistakes — a missing service name or an invalid env, format or level —
// rather than silently degrading.
func New(cfg Config) (*logger, error) {
	opts, err := cfg.resolve()
	if err != nil {
		return nil, err
	}

	levelVar := new(slog.LevelVar)
	levelVar.Set(opts.Level)

	handler := buildHandlersChain(opts, levelVar)

	base := []any{attrService, opts.Service, attrEnv, string(opts.Env)}
	if opts.Version != "" {
		base = append(base, attrVersion, opts.Version)
	}
	if opts.Instance != "" {
		base = append(base, attrInstance, opts.Instance)
	}

	return &logger{
		inner: slog.New(handler).With(base...),
		level: levelVar,
	}, nil
}

// Debug logs the message at debug level.
func (l *logger) Debug(ctx context.Context, msg string, args ...any) {
	l.inner.DebugContext(ctx, msg, args...)
}

// Info logs the message at info level.
func (l *logger) Info(ctx context.Context, msg string, args ...any) {
	l.inner.InfoContext(ctx, msg, args...)
}

// Warn logs the message at warn level.
func (l *logger) Warn(ctx context.Context, msg string, args ...any) {
	l.inner.WarnContext(ctx, msg, args...)
}

// Error logs the message at error level.
func (l *logger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, args...)
}

// Enabled reports whether the level is enabled.
func (l *logger) Enabled(ctx context.Context, level slog.Level) bool {
	return l.inner.Enabled(ctx, level)
}

// With returns a child logger carrying the given attributes on every
// record; the child shares this logger's level.
func (l *logger) With(args ...any) Logger {
	return &logger{
		inner: l.inner.With(args...),
		level: l.level,
	}
}

// WithGroup returns a child logger whose attributes appear inside the
// named group; the child shares this logger's level.
func (l *logger) WithGroup(name string) Logger {
	return &logger{
		inner: l.inner.WithGroup(name),
		level: l.level,
	}
}

// SetLevel changes the minimum level at runtime. The change is immediate
// and applies to every logger derived from this one.
func (l *logger) SetLevel(level slog.Level) {
	l.level.Set(level)
}

// Unwrap returns the underlying *slog.Logger.
func (l *logger) Unwrap() *slog.Logger {
	return l.inner
}

// buildHandlersChain composes the handler chain:
//
//	sanitize → ctx → stack → sink
func buildHandlersChain(opts options, level *slog.LevelVar) slog.Handler {
	handler := buildSink(opts, level)
	handler = stack.NewHandler(opts.StackTraceLevel, handler)
	handler = ctx.NewHandler(opts.ContextExtractors, handler)
	handler = buildSanitizeHandler(opts.Sanitize, handler)
	return handler
}

func buildSink(opts options, level *slog.LevelVar) slog.Handler {
	if opts.Format == FormatConsole {
		return buildPrettyHandler(opts, level)
	}

	return buildJSONHandler(opts, level)
}

func buildPrettyHandler(opts options, level *slog.LevelVar) *pretty.Handler {
	handlerOptions := &pretty.HandlerOptions{
		Level:     level,
		NoColor:   opts.NoColor,
		AddSource: opts.AddSource != nil && *opts.AddSource,
	}
	return pretty.NewHandler(opts.Writer, handlerOptions)
}

func buildJSONHandler(opts options, level *slog.LevelVar) *slog.JSONHandler {
	handlerOptions := &slog.HandlerOptions{
		Level:     level,
		AddSource: opts.AddSource != nil && *opts.AddSource,
	}
	return slog.NewJSONHandler(opts.Writer, handlerOptions)
}

func buildSanitizeHandler(cfg SanitizeConfig, next slog.Handler) *sanitize.Handler {
	handlerOptions := &sanitize.HandlerOptions{
		Disabled:         cfg.Disabled,
		ExtraKeys:        cfg.ExtraKeys,
		ExceptKeys:       cfg.ExceptKeys,
		DisableValueScan: cfg.DisableValueScan,
	}
	return sanitize.NewHandler(handlerOptions, next)
}
