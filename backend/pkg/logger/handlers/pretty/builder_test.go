package pretty

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/pretty/color"
	"github.com/stretchr/testify/require"
)

func Test_BuildPrettyLog_SuccessfulBuild(t *testing.T) {
	tests := []struct {
		name   string
		record slog.Record
		want   string
	}{
		{
			name:   "no attributes",
			record: newRecord(slog.LevelInfo, "hello"),
			want:   "[12:00:00.000] INFO: hello\n",
		},
		{
			name: "attributes keep order and natural value forms",
			record: newRecord(slog.LevelInfo, "msg",
				slog.Int("a", 1),
				slog.String("b", "two words"),
				slog.Duration("delay", 1500*time.Millisecond),
				slog.Any("err", errors.New("boom")),
			),
			want: "[12:00:00.000] INFO: msg\n{\n  \"a\": 1,\n  \"b\": \"two words\",\n  \"delay\": 1500000000,\n  \"err\": \"boom\"\n}\n",
		},
		{
			name: "group renders as nested braces",
			record: newRecord(slog.LevelInfo, "msg",
				slog.Group("g",
					slog.Int("x", 1),
					slog.String("y", "z w"),
				),
			),
			want: "[12:00:00.000] INFO: msg\n{\n  \"g\": {\n    \"x\": 1,\n    \"y\": \"z w\"\n  }\n}\n",
		},
		{
			name: "nested groups",
			record: newRecord(slog.LevelInfo, "msg",
				slog.Group("outer", slog.Group("inner", slog.Int("k", 1))),
			),
			want: "[12:00:00.000] INFO: msg\n{\n  \"outer\": {\n    \"inner\": {\n      \"k\": 1\n    }\n  }\n}\n",
		},
		{
			// slog itself drops empty groups in Record.AddAttrs, so an
			// empty group never reaches the handler from a record.
			name:   "empty group attr is dropped",
			record: newRecord(slog.LevelInfo, "msg", slog.Group("g")),
			want:   "[12:00:00.000] INFO: msg\n",
		},
		{
			name:   "time value uses RFC3339",
			record: newRecord(slog.LevelInfo, "msg", slog.Time("at", testTime)),
			want:   "[12:00:00.000] INFO: msg\n{\n  \"at\": \"2026-08-31T12:00:00Z\"\n}\n",
		},
		{
			name: "strings with grammar symbols are quoted",
			record: newRecord(slog.LevelInfo, "msg",
				slog.String("eq", "a=b"),
				slog.String("brace", "{x}"),
				slog.String("quote", `he"llo`),
				slog.String("backslash", `a\b`),
				slog.String("empty", ""),
				slog.String("plain", "ok"),
			),
			want: "[12:00:00.000] INFO: msg\n{\n  \"eq\": \"a=b\",\n  \"brace\": \"{x}\",\n  \"quote\": \"he\\\"llo\",\n  \"backslash\": \"a\\\\b\",\n  \"empty\": \"\",\n  \"plain\": \"ok\"\n}\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logBuilder := newBuilder(&builderOptions{Colorize: color.WithoutColorize})
			got, err := logBuilder.BuildPrettyLog(context.Background(), test.record)
			require.NoError(t, err)

			if got != test.want {
				t.Errorf("unexpected log:\nwant: %s\ngot: %s", test.want, got)
			}
		})
	}
}

func Test_BuildPrettyLog_SuccessfulBuildWithAttrsAndWithGroup(t *testing.T) {
	tests := []struct {
		name   string
		build  func(*builder) *builder
		record slog.Record
		want   string
	}{
		{
			name: "stored attrs come before record attrs",
			build: func(b *builder) *builder {
				return b.WithAttrs([]slog.Attr{slog.Int("a", 1)})
			},
			record: func() slog.Record {
				return newRecord(slog.LevelInfo, "msg", slog.Int("b", 2))
			}(),
			want: "[12:00:00.000] INFO: msg\n{\n  \"a\": 1,\n  \"b\": 2\n}\n",
		},
		{
			name: "record attrs nest in group",
			build: func(h *builder) *builder {
				return h.WithAttrs([]slog.Attr{slog.Int("a", 1)}).WithGroup("g")
			},
			record: newRecord(slog.LevelInfo, "msg", slog.Int("b", 2)),
			want:   "[12:00:00.000] INFO: msg\n{\n  \"a\": 1,\n  \"g\": {\n    \"b\": 2\n  }\n}\n",
		},
		{
			name: "stored attrs nest in group added before them",
			build: func(h *builder) *builder {
				return h.WithGroup("g").WithAttrs([]slog.Attr{slog.Int("x", 1)})
			},
			record: newRecord(slog.LevelInfo, "msg"),
			want:   "[12:00:00.000] INFO: msg\n{\n  \"g\": {\n    \"x\": 1\n  }\n}\n",
		},
		{
			name: "deep group path",
			build: func(h *builder) *builder {
				return h.WithGroup("g1").WithGroup("g2")
			},
			record: newRecord(slog.LevelInfo, "msg", slog.Int("k", 1)),
			want:   "[12:00:00.000] INFO: msg\n{\n  \"g1\": {\n    \"g2\": {\n      \"k\": 1\n    }\n  }\n}\n",
		},
		{
			name: "group path with no record attrs adds nothing",
			build: func(h *builder) *builder {
				return h.WithGroup("g")
			},
			record: newRecord(slog.LevelInfo, "msg"),
			want:   "[12:00:00.000] INFO: msg\n",
		},
		{
			name: "stored empty group is dropped",
			build: func(b *builder) *builder {
				return b.WithAttrs([]slog.Attr{slog.Group("g")})
			},
			record: newRecord(slog.LevelInfo, "msg"),
			want:   "[12:00:00.000] INFO: msg\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logBuilder := test.build(newBuilder(&builderOptions{Colorize: color.WithoutColorize}))
			got, err := logBuilder.BuildPrettyLog(context.Background(), test.record)
			require.NoError(t, err)

			if got != test.want {
				t.Errorf("unexpected log:\nwant: %s\ngot:  %s", test.want, got)
			}
		})
	}
}

func Test_BuildPrettyLog_SuccessfulAddSource(t *testing.T) {
	pc, _, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	logBuilder := newBuilder(&builderOptions{AddSource: true, Colorize: color.WithoutColorize})
	record := slog.NewRecord(testTime, slog.LevelInfo, "msg", pc)

	got, err := logBuilder.BuildPrettyLog(context.Background(), record)
	require.NoError(t, err)

	if !strings.Contains(got, "\"source\":") {
		t.Errorf("expected source attribute, but got: %q", got)
	}

	if !strings.Contains(got, "\"function\":") {
		t.Errorf("expected function attribute, but got: %q", got)
	}

	if !strings.Contains(got, "\"file\":") {
		t.Errorf("expected file attribute, but got: %q", got)
	}

	if !strings.Contains(got, "\"line\":") {
		t.Errorf("expected line attribute, but got: %q", got)
	}
}

func Test_BuildPrettyLog_SuccessfulReplaceAttr(t *testing.T) {
	options := &builderOptions{
		ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
			if len(groups) == 0 && attr.Key == slog.LevelKey {
				return slog.Attr{}
			}
			if attr.Key == "drop_me" {
				return slog.Attr{}
			}
			return attr
		},
		Colorize: color.WithoutColorize,
	}
	logBuilder := newBuilder(options)
	record := newRecord(slog.LevelInfo, "msg", slog.Int("drop_me", 0), slog.Int("keep", 1))
	want := "[12:00:00.000] msg\n{\n  \"keep\": 1\n}\n"

	got, err := logBuilder.BuildPrettyLog(context.Background(), record)
	require.NoError(t, err)

	if got != want {
		t.Errorf("unexpected log:\nwant: %s\ngot:  %s", want, got)
	}
}

func Test_BuildPrettyLog_SuccessfulColorizeLogSegments(t *testing.T) {
	logBuilder := newBuilder(&builderOptions{AddSource: true, Colorize: color.WithColorize})
	record := newRecord(slog.LevelInfo, "msg", slog.Int("a", 1))

	got, err := logBuilder.BuildPrettyLog(context.Background(), record)
	require.NoError(t, err)

	if !strings.HasPrefix(got, "\033[37m") {
		t.Errorf("expected ANSI color code in output, got %q", got)
	}

	if !strings.HasSuffix(got, "\033[0m\n") {
		t.Errorf("expected ANSI reset code in output, got %q", got)
	}
}
