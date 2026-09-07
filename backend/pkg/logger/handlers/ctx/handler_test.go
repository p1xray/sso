package ctx

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func newCtxTestLogger(extractors []ContextAttrFunc) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	sink := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(NewHandler(extractors, sink)), buf
}

func Test_CtxHandler_InjectsRequestID(t *testing.T) {
	logger, buf := newCtxTestLogger([]ContextAttrFunc{ExtractRequestID})

	ctx := WithRequestID(context.Background(), "req-42")
	logger.InfoContext(ctx, "msg", slog.String("user_id", "u-1"))

	got := buf.String()
	if !strings.Contains(got, "request_id=req-42") {
		t.Errorf("expected request_id in output, got: %s", got)
	}
	if !strings.Contains(got, "user_id=u-1") {
		t.Errorf("expected record attrs to survive, got: %s", got)
	}
}

func Test_CtxHandler_NoRequestIDAddsNothing(t *testing.T) {
	logger, buf := newCtxTestLogger([]ContextAttrFunc{ExtractRequestID})

	logger.InfoContext(context.Background(), "msg")

	if got := buf.String(); strings.Contains(got, "request_id") {
		t.Errorf("expected no request_id attr, got: %s", got)
	}
}

func Test_CtxHandler_CustomExtractors(t *testing.T) {
	trace := func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("trace_id", "trace-1")}
	}
	logger, buf := newCtxTestLogger([]ContextAttrFunc{ExtractRequestID, trace})

	ctx := WithRequestID(context.Background(), "req-42")
	logger.InfoContext(ctx, "msg")

	got := buf.String()
	if !strings.Contains(got, "request_id=req-42") || !strings.Contains(got, "trace_id=trace-1") {
		t.Errorf("expected both extracted attrs, got: %s", got)
	}
}

func Test_CtxHandler_WithKeepsLayer(t *testing.T) {
	logger, buf := newCtxTestLogger([]ContextAttrFunc{ExtractRequestID})

	derived := logger.With(slog.String("component", "api"))
	ctx := WithRequestID(context.Background(), "req-42")
	derived.InfoContext(ctx, "msg")

	got := buf.String()
	if !strings.Contains(got, "request_id=req-42") || !strings.Contains(got, "component=api") {
		t.Errorf("expected ctx extraction to survive With, got: %s", got)
	}
}
