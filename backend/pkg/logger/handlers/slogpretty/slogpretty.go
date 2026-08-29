package slogpretty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"github.com/p1xray/sso/backend/pkg/logger/color"
)

const (
	timeFormat = "[15:04:05.000]"
	lineBreak  = "\n"
)

type PrettyHandler struct {
	slog.Handler
	replaceAttr func(groups []string, a slog.Attr) slog.Attr

	buf    *bytes.Buffer
	mutex  *sync.Mutex
	writer io.Writer

	colorize         color.Colorizer
	outputEmptyAttrs bool
}

func NewPrettyHandler(
	options *slog.HandlerOptions,
	out io.Writer,
	setters ...PrettyHandlerOption,
) *PrettyHandler {
	if options == nil {
		options = &slog.HandlerOptions{}
	}

	buf := &bytes.Buffer{}
	handler := &PrettyHandler{
		Handler: slog.NewJSONHandler(buf, &slog.HandlerOptions{
			Level:       options.Level,
			AddSource:   options.AddSource,
			ReplaceAttr: suppressDefaults(options.ReplaceAttr),
		}),
		replaceAttr: options.ReplaceAttr,

		buf:    buf,
		mutex:  &sync.Mutex{},
		writer: out,

		colorize:         color.WithoutColorize,
		outputEmptyAttrs: false,
	}

	for _, setter := range setters {
		setter(handler)
	}

	return handler
}

func (h *PrettyHandler) Handle(ctx context.Context, rec slog.Record) error {
	level := h.formatLevel(rec)
	time := h.formatTime(rec)
	message := h.formatMessage(rec)
	attributes, err := h.formatAttributes(ctx, rec)
	if err != nil {
		return err
	}

	logLine := h.GenerateLogLine(level, time, message, attributes)

	_, err = io.WriteString(h.writer, logLine)
	if err != nil {
		return fmt.Errorf("error when writing to writer: %w", err)
	}

	return nil
}

func (h *PrettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &PrettyHandler{
		Handler:     h.Handler.WithAttrs(attrs),
		replaceAttr: h.replaceAttr,

		buf:    h.buf,
		mutex:  h.mutex,
		writer: h.writer,

		colorize:         h.colorize,
		outputEmptyAttrs: h.outputEmptyAttrs,
	}
}

func (h *PrettyHandler) WithGroup(name string) slog.Handler {
	return &PrettyHandler{
		Handler:     h.Handler.WithGroup(name),
		replaceAttr: h.replaceAttr,

		buf:    h.buf,
		mutex:  h.mutex,
		writer: h.writer,

		colorize:         h.colorize,
		outputEmptyAttrs: h.outputEmptyAttrs,
	}
}

func (h *PrettyHandler) computeAttrs(
	ctx context.Context,
	r slog.Record,
) (map[string]any, error) {
	h.mutex.Lock()
	defer func() {
		h.buf.Reset()
		h.mutex.Unlock()
	}()
	if err := h.Handler.Handle(ctx, r); err != nil {
		return nil, fmt.Errorf("error when calling inner handler's Handle: %w", err)
	}

	var attrs map[string]any
	err := json.Unmarshal(h.buf.Bytes(), &attrs)
	if err != nil {
		return nil, fmt.Errorf("error when unmarshaling inner handler's Handle result: %w", err)
	}
	return attrs, nil
}

func suppressDefaults(
	next func([]string, slog.Attr) slog.Attr,
) func([]string, slog.Attr) slog.Attr {
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

func (h *PrettyHandler) formatLevel(rec slog.Record) string {
	var level string
	levelAttr := slog.Attr{
		Key:   slog.LevelKey,
		Value: slog.AnyValue(rec.Level),
	}
	if h.replaceAttr != nil {
		levelAttr = h.replaceAttr([]string{}, levelAttr)
	}

	if !levelAttr.Equal(slog.Attr{}) {
		level = levelAttr.Value.String() + ":"

		if rec.Level <= slog.LevelDebug {
			level = h.colorize(color.LightGray, level)
		} else if rec.Level <= slog.LevelInfo {
			level = h.colorize(color.Cyan, level)
		} else if rec.Level < slog.LevelWarn {
			level = h.colorize(color.LightBlue, level)
		} else if rec.Level < slog.LevelError {
			level = h.colorize(color.LightYellow, level)
		} else if rec.Level <= slog.LevelError+1 {
			level = h.colorize(color.LightRed, level)
		} else if rec.Level > slog.LevelError+1 {
			level = h.colorize(color.LightMagenta, level)
		}
	}

	return level
}

func (h *PrettyHandler) formatTime(rec slog.Record) string {
	var time string
	timeAttr := slog.Attr{
		Key:   slog.TimeKey,
		Value: slog.StringValue(rec.Time.Format(timeFormat)),
	}
	if h.replaceAttr != nil {
		timeAttr = h.replaceAttr([]string{}, timeAttr)
	}
	if !timeAttr.Equal(slog.Attr{}) {
		time = h.colorize(color.LightGray, timeAttr.Value.String())
	}

	return time
}

func (h *PrettyHandler) formatMessage(rec slog.Record) string {
	var message string
	messageAttr := slog.Attr{
		Key:   slog.MessageKey,
		Value: slog.StringValue(rec.Message),
	}
	if h.replaceAttr != nil {
		messageAttr = h.replaceAttr([]string{}, messageAttr)
	}
	if !messageAttr.Equal(slog.Attr{}) {
		message = h.colorize(color.White, messageAttr.Value.String())
	}

	return message
}

func (h *PrettyHandler) formatAttributes(ctx context.Context, rec slog.Record) (string, error) {
	attrs, err := h.computeAttrs(ctx, rec)
	if err != nil {
		return "", err
	}

	var attrsAsBytes []byte
	if h.outputEmptyAttrs || len(attrs) > 0 {
		attrsAsBytes, err = json.MarshalIndent(attrs, "", "  ")
		if err != nil {
			return "", fmt.Errorf("error when marshaling attrs: %w", err)
		}
	}

	return string(attrsAsBytes), nil
}

func (h *PrettyHandler) GenerateLogLine(level, time, message, attributes string) string {
	out := strings.Builder{}
	if len(time) > 0 {
		out.WriteString(time)
		out.WriteString(" ")
	}
	if len(level) > 0 {
		out.WriteString(level)
		out.WriteString(" ")
	}
	if len(message) > 0 {
		out.WriteString(message)
		out.WriteString(" ")
	}
	if len(attributes) > 0 {
		out.WriteString(h.colorize(color.DarkGray, attributes))
	}
	out.WriteString(lineBreak)

	return strings.Trim(out.String(), " ")
}
