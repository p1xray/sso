package postgresql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/p1xray/sso/backend/pkg/logger"
)

// newBufferLogger returns a logger writing JSON records to buf.
func newBufferLogger(t *testing.T, buf io.Writer) logger.Logger {
	t.Helper()

	lg, err := logger.New(logger.Config{
		Service: "postgresql-test",
		Env:     logger.EnvDev,
		Format:  logger.FormatJSON,
		Writer:  buf,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	return lg
}

func Test_loggerTracer_TraceQueryStart(t *testing.T) {
	logs := new(bytes.Buffer)
	tracer := &loggerTracer{logger: newBufferLogger(t, logs)}

	tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT * FROM users WHERE id = $1",
		Args: []any{42},
	})

	out := logs.String()
	for _, want := range []string{"executing query", "SELECT * FROM users WHERE id = $1", "42"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want it to contain %q", out, want)
		}
	}
}

func Test_loggerTracer_TraceQueryEnd_Failure(t *testing.T) {
	logs := new(bytes.Buffer)
	tracer := &loggerTracer{logger: newBufferLogger(t, logs)}

	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{
		Err: errors.New("connection refused"),
	})

	out := logs.String()
	if !strings.Contains(out, "query failed") {
		t.Errorf("logs = %q, want a failure record", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Errorf("logs = %q, want the error", out)
	}
}

func Test_loggerTracer_TraceQueryEnd_Success(t *testing.T) {
	logs := new(bytes.Buffer)
	tracer := &loggerTracer{logger: newBufferLogger(t, logs)}

	tracer.TraceQueryEnd(context.Background(), nil, pgx.TraceQueryEndData{})

	if logs.Len() != 0 {
		t.Errorf("logs = %q, want nothing logged for a successful query", logs.String())
	}
}
