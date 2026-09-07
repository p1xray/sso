package pretty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/pretty/color"
)

const (
	timeFormat = "[15:04:05.000]"
	lineBreak  = "\n"
	emptyJSON  = "{}"
)

type builderOptions struct {
	Level       slog.Leveler
	AddSource   bool
	ReplaceAttr replaceAttrFunc
	Colorize    color.Colorizer
}

// builder renders records as console lines: attrs are serialized by an inner
// JSON handler, then indented and colorized.
type builder struct {
	opts        *builderOptions
	jsonBuf     *bytes.Buffer
	jsonHandler slog.Handler
	mu          *sync.Mutex
}

func newBuilder(options *builderOptions) *builder {
	if options == nil {
		options = &builderOptions{}
	}

	buf := &bytes.Buffer{}
	jsonHandler := slog.NewJSONHandler(buf, &slog.HandlerOptions{
		Level:       options.Level,
		AddSource:   options.AddSource,
		ReplaceAttr: suppressDefaultAttributes(options.ReplaceAttr),
	})

	return &builder{
		opts:        options,
		jsonBuf:     buf,
		jsonHandler: jsonHandler,
		mu:          &sync.Mutex{},
	}
}

// WithAttrs returns a builder with the attributes applied to its JSON handler.
func (b *builder) WithAttrs(attrs []slog.Attr) *builder {
	next := b.clone()
	next.jsonHandler = b.jsonHandler.WithAttrs(attrs)
	return next
}

// WithGroup returns a builder with the group applied to its JSON handler.
func (b *builder) WithGroup(name string) *builder {
	next := b.clone()
	next.jsonHandler = b.jsonHandler.WithGroup(name)
	return next
}

// BuildPrettyLog renders the record: time, level, message, then indented
// JSON attrs.
func (b *builder) BuildPrettyLog(ctx context.Context, rec slog.Record) (string, error) {
	log := strings.Builder{}
	if formatedTime := b.formatTime(rec); formatedTime != "" {
		log.WriteString(formatedTime)
	}

	if formatedLevel := b.formatLevel(rec); formatedLevel != "" {
		log.WriteString(" ")
		log.WriteString(formatedLevel)
	}

	if formatedMessage := b.formatMessage(rec); formatedMessage != "" {
		log.WriteString(" ")
		log.WriteString(formatedMessage)
	}

	formatedAttributes, err := b.formatAttributes(ctx, rec)
	if err != nil {
		return "", fmt.Errorf("build log: %w", err)
	}

	if formatedAttributes != "" {
		log.WriteString(lineBreak)
		log.WriteString(formatedAttributes)
	}

	log.WriteString(lineBreak)

	return log.String(), nil
}

func (b *builder) formatTime(rec slog.Record) string {
	if rec.Time.IsZero() {
		return ""
	}

	timeAttr := slog.Time(slog.TimeKey, rec.Time)
	if b.opts.ReplaceAttr != nil {
		timeAttr = b.opts.ReplaceAttr(nil, timeAttr)
		if timeAttr.Equal(slog.Attr{}) {
			return ""
		}
	}

	value := rec.Time
	if timeAttr.Value.Kind() == slog.KindTime {
		value = timeAttr.Value.Time()
	}

	return b.opts.Colorize(color.LightGray, value.Format(timeFormat))
}

func (b *builder) formatLevel(rec slog.Record) string {
	levelAttr := slog.Any(slog.LevelKey, rec.Level)
	if b.opts.ReplaceAttr != nil {
		levelAttr = b.opts.ReplaceAttr(nil, levelAttr)
		if levelAttr.Equal(slog.Attr{}) {
			return ""
		}
	}

	level := levelAttr.Value.String() + ":"

	switch {
	case rec.Level <= slog.LevelDebug:
		level = b.opts.Colorize(color.LightGray, level)
	case rec.Level <= slog.LevelInfo:
		level = b.opts.Colorize(color.Cyan, level)
	case rec.Level < slog.LevelWarn:
		level = b.opts.Colorize(color.LightBlue, level)
	case rec.Level < slog.LevelError:
		level = b.opts.Colorize(color.LightYellow, level)
	case rec.Level <= slog.LevelError+1:
		level = b.opts.Colorize(color.LightRed, level)
	default:
		level = b.opts.Colorize(color.LightMagenta, level)
	}

	return level
}

func (b *builder) formatMessage(rec slog.Record) string {
	messageAttr := slog.String(slog.MessageKey, rec.Message)
	if b.opts.ReplaceAttr != nil {
		messageAttr = b.opts.ReplaceAttr(nil, messageAttr)
		if messageAttr.Equal(slog.Attr{}) {
			return ""
		}
	}

	return b.opts.Colorize(color.White, messageAttr.Value.String())
}

func (b *builder) formatAttributes(ctx context.Context, rec slog.Record) (string, error) {
	b.mu.Lock()
	defer func() {
		b.jsonBuf.Reset()
		b.mu.Unlock()
	}()

	if err := b.resolveAttributesToJSON(ctx, rec); err != nil {
		return "", fmt.Errorf("format attributes: %w", err)
	}

	if err := b.indentJSON(); err != nil {
		return "", fmt.Errorf("format attributes: %w", err)
	}

	formattedAttributes := b.jsonBuf.String()
	formattedAttributes = strings.TrimSpace(formattedAttributes)
	formattedAttributes = strings.Trim(formattedAttributes, lineBreak)
	if formattedAttributes == emptyJSON {
		return "", nil
	}

	colorizedAttributes := b.opts.Colorize(color.DarkGray, formattedAttributes)
	return colorizedAttributes, nil
}

func (b *builder) resolveAttributesToJSON(ctx context.Context, rec slog.Record) error {
	if err := b.jsonHandler.Handle(ctx, rec); err != nil {
		return fmt.Errorf("calling inner json handler's Handle: %w", err)
	}

	return nil
}

func (b *builder) indentJSON() error {
	jsonBytes := b.jsonBuf.Bytes()
	b.jsonBuf.Reset()

	if err := json.Indent(b.jsonBuf, jsonBytes, "", "  "); err != nil {
		return fmt.Errorf("indent json: %w", err)
	}

	return nil
}

func (b *builder) clone() *builder {
	return &builder{
		opts:        b.opts,
		jsonHandler: b.jsonHandler,
		jsonBuf:     b.jsonBuf,
		mu:          b.mu, // mutex shared among all clones of this builder
	}
}

func suppressDefaultAttributes(next replaceAttrFunc) replaceAttrFunc {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey ||
			a.Key == slog.LevelKey ||
			a.Key == slog.MessageKey {
			return slog.Attr{}
		}
		if next == nil {
			return a
		}
		return next(groups, a)
	}
}
