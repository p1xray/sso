package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	logctx "github.com/p1xray/sso/backend/pkg/logger/handlers/ctx"
)

func newTestLogger(t *testing.T, cfg Config) (*logger, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	cfg.Writer = buf

	lg, err := New(cfg)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	return lg, buf
}

func Test_New_JSONBaseAttrs(t *testing.T) {
	lg, buf := newTestLogger(t, Config{
		Service: "auth",
		Env:     EnvProd,
		Version: "v1.2.3",
		Format:  FormatJSON,
	})

	lg.Info(context.Background(), "login handled", slog.String("user_id", "u-1"))

	line := buf.String()
	var fields map[string]any
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, line)
	}
	if fields["service"] != "auth" || fields["env"] != "prod" || fields["version"] != "v1.2.3" {
		t.Errorf("missing base attrs: %v", fields)
	}
	if fields["user_id"] != "u-1" {
		t.Errorf("missing record attr: %v", fields)
	}

	// Base attrs precede record attrs in the JSON line.
	if strings.Index(line, `"service"`) > strings.Index(line, `"user_id"`) {
		t.Errorf("base attrs must precede record attrs: %s", line)
	}
}

func Test_New_VersionOmittedWhenEmpty(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvDev})

	lg.Info(context.Background(), "msg")

	if strings.Contains(buf.String(), "version") {
		t.Errorf("version must be omitted when empty, got: %s", buf.String())
	}
}

func Test_New_MaskingEndToEnd(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd})

	lg.Info(context.Background(), "token exchange",
		slog.String("client_id", "web-app"),
		slog.String("access_token", "eyJhbGciOiJIUzI1NiJ9.sig"),
	)

	line := buf.String()
	if !strings.Contains(line, `"client_id":"web-app"`) {
		t.Errorf("client_id must survive, got: %s", line)
	}
	if !strings.Contains(line, `"access_token":"[REDACTED]"`) {
		t.Errorf("access_token must be masked, got: %s", line)
	}
}

func Test_New_RequestIDEndToEnd(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd})

	ctx := logctx.WithRequestID(context.Background(), "req-42")
	lg.Info(ctx, "msg")

	if !strings.Contains(buf.String(), `"request_id":"req-42"`) {
		t.Errorf("expected request_id in JSON output, got: %s", buf.String())
	}
}

func Test_New_StackOnError(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd})

	lg.Error(context.Background(), "boom", slog.String("error", "db down"))
	errorLine := buf.String()
	if !strings.Contains(errorLine, `"stack_trace"`) {
		t.Errorf("expected stack attr on error record, got: %s", errorLine)
	}

	buf.Reset()
	lg.Info(context.Background(), "fine")
	if strings.Contains(buf.String(), `"stack_trace"`) {
		t.Errorf("info record must not carry a stack, got: %s", buf.String())
	}
}

func Test_New_SetLevelDynamic(t *testing.T) {
	lg, buf := newTestLogger(t, Config{
		Service: "auth",
		Env:     EnvProd,
		Level:   LevelDebug,
	})
	ctx := context.Background()

	lg.Debug(ctx, "visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Fatal("debug must pass at debug level")
	}

	lg.SetLevel(slog.LevelWarn)
	buf.Reset()

	lg.Debug(ctx, "hidden")
	lg.Info(ctx, "hidden too")
	if buf.Len() != 0 {
		t.Errorf("records below warn must be dropped after SetLevel, got: %s", buf.String())
	}

	lg.Warn(ctx, "visible again")
	if !strings.Contains(buf.String(), "visible again") {
		t.Error("warn must pass after SetLevel")
	}
}

func Test_New_ConsoleFormat(t *testing.T) {
	lg, buf := newTestLogger(t, Config{
		Service: "auth",
		Env:     EnvLocal,
		Format:  FormatConsole,
		NoColor: true,
	})

	ctx := logctx.WithRequestID(context.Background(), "req-42")
	lg.Info(ctx, "login handled", slog.String("user_id", "u-1"))

	line := buf.String()
	if !strings.Contains(line, "INFO: login handled") {
		t.Errorf("expected pretty line, got: %s", line)
	}
	// Attrs render as an indented JSON block; check presence and order.
	wantAttrs := []string{
		`"service": "auth"`,
		`"env": "local"`,
		`"request_id": "req-42"`,
		`"user_id": "u-1"`,
	}
	last := -1
	for _, want := range wantAttrs {
		i := strings.Index(line, want)
		if i == -1 {
			t.Errorf("expected %s on the line, got: %s", want, line)
			continue
		}
		if i < last {
			t.Errorf("attrs out of order, %s came too early: %s", want, line)
		}
		last = i
	}
	if strings.Contains(line, "\033[") {
		t.Errorf("expected no ANSI codes with NoColor, got: %q", line)
	}
}

func Test_New_ConsoleFormatColoredByDefault(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvLocal, Format: FormatConsole})

	lg.Info(context.Background(), "msg")

	if !strings.Contains(buf.String(), "\033[") {
		t.Errorf("expected ANSI codes by default in local env, got: %q", buf.String())
	}
}

func Test_New_ContextAttrsReplaceDefault(t *testing.T) {
	trace := func(ctx context.Context) []slog.Attr {
		return []slog.Attr{slog.String("trace_id", "trace-1")}
	}

	lg, buf := newTestLogger(t, Config{
		Service:      "auth",
		Env:          EnvProd,
		ContextAttrs: []logctx.ContextAttrFunc{trace},
	})

	ctx := logctx.WithRequestID(context.Background(), "req-42")
	lg.Info(ctx, "msg")

	line := buf.String()
	if !strings.Contains(line, `"trace_id":"trace-1"`) {
		t.Errorf("expected custom extractor attr, got: %s", line)
	}
	if strings.Contains(line, "request_id") {
		t.Errorf("custom ContextAttrs must replace the default extractor, got: %s", line)
	}
}

func Test_New_ValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "empty service",
			config: Config{Env: EnvLocal},
		},
		{
			name:   "invalid env",
			config: Config{Service: "auth", Env: Env("staging")},
		},
		{
			name:   "invalid format",
			config: Config{Service: "auth", Env: EnvLocal, Format: Format("xml")},
		},
		{
			name:   "invalid level",
			config: Config{Service: "auth", Env: EnvLocal, Level: Level("loud")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lg, err := New(test.config)
			if err == nil {
				t.Error("expected an error, got nil")
			}
			if lg != nil {
				t.Errorf("expected nil logger on error, got %v", lg)
			}
		})
	}
}

func Test_Unwrap_Successful(t *testing.T) {
	lg, _ := newTestLogger(t, Config{
		Service: "auth",
		Env:     EnvProd,
		Level:   LevelWarn,
	})

	if lg.Unwrap() == nil || lg.Unwrap() != lg.inner {
		t.Error("Unwrap must return the inner slog logger")
	}
}

func Test_Logger_InterfaceConsumption(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd})

	// The way services hold the logger: the interface, not the concrete.
	var log Logger = lg

	ctx := logctx.WithRequestID(context.Background(), "req-42")
	log.Info(ctx, "token exchange",
		slog.String("access_token", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.sig"),
	)

	line := buf.String()
	if !strings.Contains(line, `"request_id":"req-42"`) {
		t.Errorf("expected request_id through the interface, got: %s", line)
	}
	if !strings.Contains(line, `"access_token":"[REDACTED]"`) {
		t.Errorf("expected masking through the interface, got: %s", line)
	}
}

func Test_Logger_WithThroughInterface(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd, Level: LevelDebug})

	var log Logger = lg
	child := log.With("component", "storage")

	ctx := logctx.WithRequestID(context.Background(), "req-42")
	child.Debug(ctx, "query slow", slog.String("client_secret", "leak-me"))

	line := buf.String()
	for _, want := range []string{
		`"component":"storage"`,
		`"request_id":"req-42"`,
		`"client_secret":"[REDACTED]"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("expected %s in child logger output, got: %s", want, line)
		}
	}

	// Children share the parent's level variable.
	log.(Leveler).SetLevel(slog.LevelWarn)
	buf.Reset()
	child.Debug(ctx, "hidden")
	if buf.Len() != 0 {
		t.Errorf("child must follow the parent level, got: %s", buf.String())
	}

	// SetLevel is on the interface too.
	child.(Leveler).SetLevel(slog.LevelDebug)
	child.Debug(ctx, "visible")
	if !strings.Contains(buf.String(), "visible") {
		t.Error("child must honor its own SetLevel via the interface")
	}
}

func Test_Logger_WithGroupThroughInterface(t *testing.T) {
	lg, buf := newTestLogger(t, Config{Service: "auth", Env: EnvProd})

	var log Logger = lg
	child := log.WithGroup("http")

	child.Info(context.Background(), "request",
		slog.String("method", "POST"),
		slog.String("authorization", "Bearer leak-me"),
	)

	line := buf.String()
	if !strings.Contains(line, `"http":{"method":"POST","authorization":"[REDACTED]"}`) {
		t.Errorf("expected grouped and masked attrs through the interface, got: %s", line)
	}
}
