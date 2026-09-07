package pretty

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/pretty/color"
)

type replaceAttrFunc func(groups []string, a slog.Attr) slog.Attr

// HandlerOptions are options for a pretty handler. A zero HandlerOptions consists
// entirely of default values.
type HandlerOptions struct {
	// Level reports the minimum record level that will be logged. If Level is nil,
	// the handler assumes LevelInfo.
	Level slog.Leveler

	// NoColor disables ANSI colors of the console format.
	NoColor bool

	// AddSource causes the handler to compute the source code position of the log
	// statement and add a SourceKey attribute to the output.
	AddSource bool

	// ReplaceAttr is called to rewrite each non-group attribute before it is logged.
	// If ReplaceAttr returns a zero Attr, the attribute is discarded.
	ReplaceAttr replaceAttrFunc
}

// Handler is a slog.Handler with human-friendly console output. Structure is
// immutable: WithAttrs and WithGroup return modified copies.
type Handler struct {
	level      slog.Leveler
	logBuilder *builder
	mu         *sync.Mutex
	writer     io.Writer
}

// NewHandler returns a Handler writing to out. A nil options means defaults:
// level info, no source, no ReplaceAttr. options.Level may be a *slog.LevelVar
// for dynamic level control.
func NewHandler(
	writer io.Writer,
	options *HandlerOptions,
) *Handler {
	if options == nil {
		options = &HandlerOptions{}
	}

	colorize := color.WithColorize
	if options.NoColor {
		colorize = color.WithoutColorize
	}

	logBuilder := newBuilder(&builderOptions{
		Level:       options.Level,
		AddSource:   options.AddSource,
		ReplaceAttr: options.ReplaceAttr,
		Colorize:    colorize,
	})

	handler := &Handler{
		level:      options.Level,
		logBuilder: logBuilder,
		mu:         &sync.Mutex{},
		writer:     writer,
	}

	return handler
}

// Enabled reports whether the given level passes the handler level.
func (h *Handler) Enabled(_ context.Context, level slog.Level) bool {
	return h.enabled(level)
}

func (h *Handler) enabled(level slog.Level) bool {
	if h.level == nil {
		return level >= slog.LevelInfo
	}

	return level >= h.level.Level()
}

// WithAttrs returns a new Handler whose attributes consist of both the
// receiver's attributes and the arguments.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := h.clone()
	next.logBuilder = h.logBuilder.WithAttrs(attrs)
	return next
}

// WithGroup returns a new Handler with the given group appended to the
// receiver's existing groups. The keys of all subsequent attributes, whether
// added by With or in a Record, should be qualified by the sequence of group
// names.
func (h *Handler) WithGroup(name string) slog.Handler {
	next := h.clone()
	next.logBuilder = h.logBuilder.WithGroup(name)
	return next
}

// Handle formats the log entry into a colorful, human-readable format with
// structured attributes and writes it to the IO writer.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if !h.enabled(rec.Level) {
		return nil
	}

	log, err := h.logBuilder.BuildPrettyLog(ctx, rec)
	if err != nil {
		return fmt.Errorf("pretty handler: %w", err)
	}

	if err = h.write(log); err != nil {
		return fmt.Errorf("pretty handler: %w", err)
	}

	return nil
}

func (h *Handler) write(log string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.writer.Write([]byte(log)); err != nil {
		return fmt.Errorf("write log: %w", err)
	}

	return nil
}

func (h *Handler) clone() *Handler {
	return &Handler{
		level:      h.level,
		logBuilder: h.logBuilder,
		mu:         h.mu, // mutex shared among all clones of this handler
		writer:     h.writer,
	}
}
