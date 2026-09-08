package postgresql

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Test_New_InvalidURL(t *testing.T) {
	_, err := New(context.Background(), "://invalid")
	if err == nil {
		t.Fatal("New() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "postgresql: parse config") {
		t.Errorf("New() error = %q, want it wrapped by the package", err)
	}
}

func Test_New_InvalidOption(t *testing.T) {
	tests := []struct {
		name    string
		option  Option
		wantErr string
	}{
		{
			name:    "connection attempts below 1",
			option:  ConnAttempts(0),
			wantErr: "connection attempts must be at least 1",
		},
		{
			name:    "non-positive attempt timeout",
			option:  ConnAttemptTimeout(-time.Second),
			wantErr: "connection attempt timeout must be positive",
		},
		{
			name:    "non-positive max backoff",
			option:  MaxBackoff(-time.Second),
			wantErr: "connection backoff must be positive",
		},
		{
			name:    "nil query logger",
			option:  LogQueries(nil),
			wantErr: "query logger is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), "postgres://user:pass@localhost:5432/db?sslmode=disable", tt.option)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func Test_New_UnreachableDatabase(t *testing.T) {
	logs := new(bytes.Buffer)

	url := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", freePort(t))

	_, err := New(context.Background(), url,
		ConnAttempts(2),
		ConnAttemptTimeout(10*time.Millisecond),
		LogPingAttempts(newBufferLogger(t, logs)),
	)
	if err == nil {
		t.Fatal("New() error = nil, want ping error")
	}
	if !strings.Contains(err.Error(), "postgresql: ping after 2 attempts") {
		t.Errorf("New() error = %q, want it to report the attempts", err)
	}
	if !strings.Contains(logs.String(), "database is unreachable") {
		t.Errorf("logs = %q, want a warning about the unreachable database", logs.String())
	}
}

func Test_MaxBackoff_SetsValue(t *testing.T) {
	p := &postgres{}

	if err := MaxBackoff(3 * time.Second)(p); err != nil {
		t.Fatalf("MaxBackoff() error = %v", err)
	}
	if p.maxBackoff != 3*time.Second {
		t.Errorf("maxBackoff = %s, want %s", p.maxBackoff, 3*time.Second)
	}
}

func Test_MaxBackoff_RejectsNonPositive(t *testing.T) {
	p := &postgres{maxBackoff: time.Second}

	err := MaxBackoff(0)(p)
	if err == nil {
		t.Fatal("MaxBackoff() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "connection backoff must be positive") {
		t.Errorf("MaxBackoff() error = %q, want the validation message", err)
	}
	if p.maxBackoff != time.Second {
		t.Errorf("maxBackoff = %s, want it unchanged", p.maxBackoff)
	}
}

func Test_MaxBackoff_BoundsRetryDelay(t *testing.T) {
	url := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", freePort(t))

	// A 1s initial delay would push every wait to the 10s default cap; the 20ms
	// cap keeps all three attempts well under a second.
	start := time.Now()
	_, err := New(context.Background(), url,
		ConnAttempts(3),
		ConnAttemptTimeout(time.Hour),
		MaxBackoff(20*time.Millisecond),
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("New() error = nil, want ping error")
	}
	if !strings.Contains(err.Error(), "ping after 3 attempts") {
		t.Errorf("New() error = %q, want it to report the attempts", err)
	}
	if elapsed > time.Second {
		t.Errorf("New() took %s, want the retry delay capped near 20ms", elapsed)
	}
}

func Test_LogQueries_SetsTracer(t *testing.T) {
	lg := newBufferLogger(t, io.Discard)
	p := &postgres{poolConfig: &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}}

	if err := LogQueries(lg)(p); err != nil {
		t.Fatalf("LogQueries() error = %v", err)
	}

	tracer, ok := p.poolConfig.ConnConfig.Tracer.(*loggerTracer)
	if !ok {
		t.Fatalf("Tracer type = %T, want *loggerTracer", p.poolConfig.ConnConfig.Tracer)
	}
	if tracer.logger != lg {
		t.Error("tracer logger does not match the configured one")
	}
}

func Test_LogQueries_RejectsNil(t *testing.T) {
	p := &postgres{poolConfig: &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}}

	err := LogQueries(nil)(p)
	if err == nil {
		t.Fatal("LogQueries() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "query logger is required") {
		t.Errorf("LogQueries() error = %q, want the validation message", err)
	}
	if p.poolConfig.ConnConfig.Tracer != nil {
		t.Errorf("Tracer = %v, want it unset", p.poolConfig.ConnConfig.Tracer)
	}
}

// freePort returns a localhost port that is currently free: the listener is
// closed before the port is used, so connecting to it fails fast with
// connection refused, without any database running.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener error = %v", err)
	}

	return port
}

// Test_Integration_PostgresLifecycle runs against a real database and is
// skipped unless TEST_POSTGRESQL_DSN is set, e.g.:
//
//	TEST_POSTGRESQL_DSN="postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable" go test -run Integration -v ./...
func Test_Integration_PostgresLifecycle(t *testing.T) {
	url := os.Getenv("TEST_POSTGRESQL_DSN")
	if url == "" {
		t.Skip("TEST_POSTGRESQL_DSN is not set")
	}

	ctx := context.Background()

	logs := new(bytes.Buffer)
	pg, err := New(ctx, url, LogQueries(newBufferLogger(t, logs)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer pg.Close()

	var got int
	if err := pg.QueryRow(ctx, "SELECT 1").Scan(&got); err != nil {
		t.Fatalf("QueryRow() error = %v", err)
	}
	if got != 1 {
		t.Errorf("SELECT 1 = %d, want 1", got)
	}
	for _, want := range []string{"executing query", "SELECT 1"} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("logs = %q, want it to contain %q", logs.String(), want)
		}
	}

	if err := pg.BeginFunc(ctx, func(tx pgx.Tx) error {
		var txOne int
		return tx.QueryRow(ctx, "SELECT 1").Scan(&txOne)
	}); err != nil {
		t.Fatalf("BeginFunc() error = %v", err)
	}

	if err := pg.Ping(ctx); err != nil {
		t.Errorf("Ping() error = %v", err)
	}

	if pg.Unwrap() == nil {
		t.Error("Unwrap() = nil, want the underlying pool")
	}
	if pg.Stat() == nil {
		t.Error("Stat() = nil, want statistics")
	}
}
