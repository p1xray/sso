package sanitize

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// newSanitizeTestLogger builds a logger whose records pass the sanitizer
// and render through a plain text sink, so assertions work on key=value
// lines.
func newSanitizeTestLogger(opts *HandlerOptions) (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	sink := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(NewHandler(opts, sink)), buf
}

func toAnySlice(attrs []slog.Attr) []any {
	args := make([]any, len(attrs))
	for i, attr := range attrs {
		args[i] = attr
	}
	return args
}

func Test_Sanitize_DefaultKeysMasked(t *testing.T) {
	for _, key := range defaultSanitizeKeys {
		t.Run(key, func(t *testing.T) {
			logger, buf := newSanitizeTestLogger(&HandlerOptions{})
			logger.Info("msg", slog.String(key, "sensitive-value"))

			got := buf.String()
			if !strings.Contains(got, fmt.Sprintf("%s=[REDACTED]", key)) {
				t.Errorf("expected %s masked, got: %s", key, got)
			}
			if strings.Contains(got, "sensitive-value") {
				t.Errorf("sensitive value leaked: %s", got)
			}
		})
	}
}

func Test_Sanitize_CaseInsensitiveExactKeyMatch(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{})

	logger.Info("msg",
		slog.String("Password", "leak-me"),
		slog.String("ACCESS_TOKEN", "leak-me"),
		slog.Int("status_code", 200),
		slog.String("error_code", "E_401"),
		slog.String("http_code", "200"),
	)

	got := buf.String()
	for _, masked := range []string{"Password=[REDACTED]", "ACCESS_TOKEN=[REDACTED]"} {
		if !strings.Contains(got, masked) {
			t.Errorf("expected %s in output, got: %s", masked, got)
		}
	}
	if strings.Contains(got, "leak-me") {
		t.Errorf("sensitive value leaked: %s", got)
	}
	for _, kept := range []string{"status_code=200", "error_code=E_401", "http_code=200"} {
		if !strings.Contains(got, kept) {
			t.Errorf("expected %s to survive exact-match masking, got: %s", kept, got)
		}
	}
}

func Test_Sanitize_ExtraAndExceptKeys(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{
		ExtraKeys:  []string{"redirect_uri", "State"},
		ExceptKeys: []string{"code", "SESSION_ID"},
	})

	logger.Info("msg",
		slog.String("redirect_uri", "https://app.example/cb"),
		slog.String("state", "xyz"),
		slog.String("code", "auth-code-1"),
		slog.String("session_id", "sess-1"),
	)

	got := buf.String()
	for _, masked := range []string{"redirect_uri=[REDACTED]", "state=[REDACTED]"} {
		if !strings.Contains(got, masked) {
			t.Errorf("expected %s in output, got: %s", masked, got)
		}
	}
	for _, kept := range []string{"code=auth-code-1", "session_id=sess-1"} {
		if !strings.Contains(got, kept) {
			t.Errorf("expected %s to survive ExceptKeys, got: %s", kept, got)
		}
	}
}

func Test_Sanitize_NestedGroups(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{})

	logger.Info("msg",
		slog.Group("http",
			slog.String("method", "POST"),
			slog.String("authorization", "Bearer leak-me"),
			slog.Group("body",
				slog.String("password", "leak-me"),
				slog.String("grant_type", "authorization_code"),
			),
		),
	)

	got := buf.String()
	if !strings.Contains(got, "authorization=[REDACTED]") {
		t.Errorf("expected nested authorization masked, got: %s", got)
	}
	if !strings.Contains(got, "password=[REDACTED]") {
		t.Errorf("expected deeply nested password masked, got: %s", got)
	}
	if !strings.Contains(got, "method=POST") || !strings.Contains(got, "grant_type=authorization_code") {
		t.Errorf("expected non-sensitive group fields to survive, got: %s", got)
	}
}

func Test_Sanitize_NonStringKindsMasked(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{})

	logger.Info("msg",
		slog.Int("password", 12345),
		slog.Any("access_token", []string{"a", "b"}),
	)

	got := buf.String()
	if !strings.Contains(got, "password=[REDACTED]") {
		t.Errorf("expected int password masked, got: %s", got)
	}
	if !strings.Contains(got, "access_token=[REDACTED]") {
		t.Errorf("expected slice access_token masked, got: %s", got)
	}
}

func Test_Sanitize_ValueScan(t *testing.T) {
	const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N0XgL0n3I9PlFUP0THsR8U"

	tests := []struct {
		name  string
		attrs func() []slog.Attr
		want  []string
	}{
		{
			name:  "standalone JWT",
			attrs: func() []slog.Attr { return []slog.Attr{slog.String("assertion", jwt)} },
			want:  []string{`assertion=[REDACTED]`},
		},
		{
			name: "JWT embedded in a sentence",
			attrs: func() []slog.Attr {
				return []slog.Attr{slog.String("note", "rejected token "+jwt+" at gate")}
			},
			want: []string{`note="rejected token [REDACTED] at gate"`},
		},
		{
			name: "bearer and basic schemes",
			attrs: func() []slog.Attr {
				return []slog.Attr{
					slog.String("header", "Bearer AbCdEf123456"),
					slog.String("proxy_header", "basic dXNlcjpwYXNz"),
				}
			},
			want: []string{`header="Bearer [REDACTED]"`, `proxy_header="basic [REDACTED]"`},
		},
		{
			name: "JWT inside an error value",
			attrs: func() []slog.Attr {
				return []slog.Attr{slog.Any("err", errors.New("upstream rejected "+jwt))}
			},
			want: []string{`err="upstream rejected [REDACTED]"`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, buf := newSanitizeTestLogger(&HandlerOptions{})
			logger.Info("msg", toAnySlice(test.attrs())...)

			got := buf.String()
			for _, want := range test.want {
				if !strings.Contains(got, want) {
					t.Errorf("expected %s in output, got: %s", want, got)
				}
			}
			if strings.Contains(got, jwt) {
				t.Errorf("JWT leaked: %s", got)
			}
			if strings.Contains(got, "AbCdEf123456") {
				t.Errorf("bearer token leaked: %s", got)
			}
		})
	}
}

func Test_Sanitize_DisableValueScan(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{DisableValueScan: true})

	logger.Info("msg",
		slog.String("assertion", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3"),
		slog.String("password", "still-masked"),
	)

	got := buf.String()
	if !strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("value scan should be off, got: %s", got)
	}
	if !strings.Contains(got, "password=[REDACTED]") {
		t.Errorf("key masking should stay on, got: %s", got)
	}
}

func Test_Sanitize_Disabled(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{Disabled: true})

	logger.Info("msg", slog.String("password", "hunter2"))

	if got := buf.String(); !strings.Contains(got, "password=hunter2") {
		t.Errorf("masking should be fully disabled, got: %s", got)
	}
}

func Test_Sanitize_WithAttrsAndWithGroupKeepLayer(t *testing.T) {
	logger, buf := newSanitizeTestLogger(&HandlerOptions{})

	derived := logger.
		With(slog.String("client_secret", "stored-secret")).
		WithGroup("request")
	derived.Info("msg",
		slog.String("code", "record-code"),
		slog.String("user_id", "u-1"),
	)

	got := buf.String()
	for _, masked := range []string{
		"client_secret=[REDACTED]",
		"code=[REDACTED]",
	} {
		if !strings.Contains(got, masked) {
			t.Errorf("expected %s in output, got: %s", masked, got)
		}
	}
	if !strings.Contains(got, "request.code=[REDACTED]") ||
		!strings.Contains(got, "request.user_id=u-1") {
		t.Errorf("expected group rendering with masked code, got: %s", got)
	}
	if strings.Contains(got, "stored-secret") || strings.Contains(got, "record-code") {
		t.Errorf("sensitive value leaked through derived logger: %s", got)
	}
}

func Test_Sanitize_PreservesCallerPC(t *testing.T) {
	buf := &bytes.Buffer{}
	sink := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug, AddSource: true})
	logger := slog.New(NewHandler(nil, sink))

	logger.Info("msg", slog.String("password", "x"))

	got := buf.String()
	if !strings.Contains(got, "source=") || !strings.Contains(got, "handler_test.go") {
		t.Errorf("expected source to point at the test file, got: %s", got)
	}
}
