package grpcclient

import (
	"context"
	"crypto/tls"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"google.golang.org/grpc"
)

func Test_WithTarget_SetsValue(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{
			name:   "host and port",
			target: "127.0.0.1:9090",
		},
		{
			name:   "dns resolver",
			target: "dns:///auth.sso:9090",
		},
		{
			name:   "unix socket",
			target: "unix:///tmp/sso.sock",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{}

			if err := WithTarget(tt.target)(c); err != nil {
				t.Fatalf("WithTarget() error = %v", err)
			}
			if c.target != tt.target {
				t.Errorf("target = %q, want %q", c.target, tt.target)
			}
		})
	}
}

func Test_WithTarget_RejectsEmpty(t *testing.T) {
	c := &client{}

	err := WithTarget("")(c)
	if err == nil {
		t.Fatal("WithTarget() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "target is required") {
		t.Errorf("WithTarget() error = %q, want the validation message", err)
	}
	if c.target != "" {
		t.Errorf("target = %q, want it unchanged", c.target)
	}
}

func Test_WithInsecure_SetsValue(t *testing.T) {
	c := &client{}

	if err := WithInsecure()(c); err != nil {
		t.Fatalf("WithInsecure() error = %v", err)
	}
	if c.creds == nil {
		t.Fatal("creds = nil, want the insecure credentials")
	}
	if got := c.creds.Info().SecurityProtocol; got != "insecure" {
		t.Errorf("security protocol = %q, want %q", got, "insecure")
	}
}

func Test_WithInsecure_RejectsAfterTLS(t *testing.T) {
	c := &client{}

	if err := WithTLS(&tls.Config{})(c); err != nil {
		t.Fatalf("WithTLS() error = %v", err)
	}

	err := WithInsecure()(c)
	if err == nil {
		t.Fatal("WithInsecure() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "transport security is already configured") {
		t.Errorf("WithInsecure() error = %q, want the validation message", err)
	}
	if got := c.creds.Info().SecurityProtocol; got != "tls" {
		t.Errorf("security protocol = %q, want the TLS credentials to stay", got)
	}
}

func Test_WithTLS_SetsValue(t *testing.T) {
	c := &client{}

	if err := WithTLS(&tls.Config{})(c); err != nil {
		t.Fatalf("WithTLS() error = %v", err)
	}
	if c.creds == nil {
		t.Fatal("creds = nil, want the TLS credentials")
	}
	if got := c.creds.Info().SecurityProtocol; got != "tls" {
		t.Errorf("security protocol = %q, want %q", got, "tls")
	}
}

func Test_WithTLS_RejectsNil(t *testing.T) {
	c := &client{}

	err := WithTLS(nil)(c)
	if err == nil {
		t.Fatal("WithTLS() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "TLS config is required") {
		t.Errorf("WithTLS() error = %q, want the validation message", err)
	}
	if c.creds != nil {
		t.Error("creds = non-nil, want it unset")
	}
}

func Test_WithTLS_RejectsAfterInsecure(t *testing.T) {
	c := &client{}

	if err := WithInsecure()(c); err != nil {
		t.Fatalf("WithInsecure() error = %v", err)
	}

	err := WithTLS(&tls.Config{})(c)
	if err == nil {
		t.Fatal("WithTLS() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "transport security is already configured") {
		t.Errorf("WithTLS() error = %q, want the validation message", err)
	}
	if got := c.creds.Info().SecurityProtocol; got != "insecure" {
		t.Errorf("security protocol = %q, want the insecure credentials to stay", got)
	}
}

func Test_WithConnectAttempts_SetsValue(t *testing.T) {
	c := &client{}

	if err := WithConnectAttempts(3)(c); err != nil {
		t.Fatalf("WithConnectAttempts() error = %v", err)
	}
	if c.connectAttempts != 3 {
		t.Errorf("connectAttempts = %d, want 3", c.connectAttempts)
	}
}

func Test_WithConnectAttempts_RejectsBelowOne(t *testing.T) {
	tests := []struct {
		name     string
		attempts int
	}{
		{
			name:     "zero",
			attempts: 0,
		},
		{
			name:     "negative",
			attempts: -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{connectAttempts: 10}

			err := WithConnectAttempts(tt.attempts)(c)
			if err == nil {
				t.Fatal("WithConnectAttempts() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "connection attempts must be at least 1") {
				t.Errorf("WithConnectAttempts() error = %q, want the validation message", err)
			}
			if c.connectAttempts != 10 {
				t.Errorf("connectAttempts = %d, want it unchanged", c.connectAttempts)
			}
		})
	}
}

func Test_WithConnectAttemptTimeout_SetsValue(t *testing.T) {
	c := &client{}

	if err := WithConnectAttemptTimeout(3 * time.Second)(c); err != nil {
		t.Fatalf("WithConnectAttemptTimeout() error = %v", err)
	}
	if c.connectAttemptTimeout != 3*time.Second {
		t.Errorf("connectAttemptTimeout = %s, want %s", c.connectAttemptTimeout, 3*time.Second)
	}
}

func Test_WithConnectAttemptTimeout_RejectsNonPositive(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "zero",
			timeout: 0,
		},
		{
			name:    "negative",
			timeout: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{connectAttemptTimeout: time.Second}

			err := WithConnectAttemptTimeout(tt.timeout)(c)
			if err == nil {
				t.Fatal("WithConnectAttemptTimeout() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "connection attempt timeout must be positive") {
				t.Errorf("WithConnectAttemptTimeout() error = %q, want the validation message", err)
			}
			if c.connectAttemptTimeout != time.Second {
				t.Errorf("connectAttemptTimeout = %s, want it unchanged", c.connectAttemptTimeout)
			}
		})
	}
}

func Test_WithMaxBackoff_SetsValue(t *testing.T) {
	c := &client{}

	if err := WithMaxBackoff(3 * time.Second)(c); err != nil {
		t.Fatalf("WithMaxBackoff() error = %v", err)
	}
	if c.maxBackoff != 3*time.Second {
		t.Errorf("maxBackoff = %s, want %s", c.maxBackoff, 3*time.Second)
	}
}

func Test_WithMaxBackoff_RejectsNonPositive(t *testing.T) {
	tests := []struct {
		name    string
		backoff time.Duration
	}{
		{
			name:    "zero",
			backoff: 0,
		},
		{
			name:    "negative",
			backoff: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{maxBackoff: 10 * time.Second}

			err := WithMaxBackoff(tt.backoff)(c)
			if err == nil {
				t.Fatal("WithMaxBackoff() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "connection backoff must be positive") {
				t.Errorf("WithMaxBackoff() error = %q, want the validation message", err)
			}
			if c.maxBackoff != 10*time.Second {
				t.Errorf("maxBackoff = %s, want it unchanged", c.maxBackoff)
			}
		})
	}
}

func Test_WithLogger_SetsValue(t *testing.T) {
	lg := newBufferLogger(t, io.Discard)
	c := &client{}

	if err := WithLogger(lg)(c); err != nil {
		t.Fatalf("WithLogger() error = %v", err)
	}
	if c.logger != lg {
		t.Error("logger does not match the configured one")
	}
}

func Test_WithLogger_RejectsNil(t *testing.T) {
	c := &client{}

	err := WithLogger(nil)(c)
	if err == nil {
		t.Fatal("WithLogger() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "lifecycle logger is required") {
		t.Errorf("WithLogger() error = %q, want the validation message", err)
	}
	if c.logger != nil {
		t.Error("logger = non-nil, want it unset")
	}
}

func Test_WithGRPCOptions_Appends(t *testing.T) {
	c := &client{}

	if err := WithGRPCOptions(grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024)))(c); err != nil {
		t.Fatalf("WithGRPCOptions() error = %v", err)
	}
	if err := WithGRPCOptions(
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(2048)),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(4096)),
	)(c); err != nil {
		t.Fatalf("WithGRPCOptions() error = %v", err)
	}

	if len(c.grpcOptions) != 3 {
		t.Errorf("grpcOptions count = %d, want 3", len(c.grpcOptions))
	}
}

func Test_New_RequiresTarget(t *testing.T) {
	_, err := New(context.Background(), WithInsecure())
	if err == nil {
		t.Fatal("New() error = nil, want target error")
	}
	if !strings.Contains(err.Error(), "grpcclient: target is required") {
		t.Errorf("New() error = %q, want the target error wrapped by the package", err)
	}
}

func Test_New_RequiresSecurityChoice(t *testing.T) {
	_, err := New(context.Background(), WithTarget("127.0.0.1:9090"))
	if err == nil {
		t.Fatal("New() error = nil, want security choice error")
	}
	if !strings.Contains(err.Error(), "grpcclient: transport security choice is required") {
		t.Errorf("New() error = %q, want the security choice error wrapped by the package", err)
	}
}

func Test_New_InvalidOption(t *testing.T) {
	tests := []struct {
		name    string
		option  Option
		wantErr string
	}{
		{
			name:    "empty target",
			option:  WithTarget(""),
			wantErr: "target is required",
		},
		{
			name:    "nil TLS config",
			option:  WithTLS(nil),
			wantErr: "TLS config is required",
		},
		{
			name:    "connection attempts below one",
			option:  WithConnectAttempts(0),
			wantErr: "connection attempts must be at least 1",
		},
		{
			name:    "non-positive connection attempt timeout",
			option:  WithConnectAttemptTimeout(-time.Second),
			wantErr: "connection attempt timeout must be positive",
		},
		{
			name:    "non-positive connection backoff",
			option:  WithMaxBackoff(0),
			wantErr: "connection backoff must be positive",
		},
		{
			name:    "nil lifecycle logger",
			option:  WithLogger(nil),
			wantErr: "lifecycle logger is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.option)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// newBufferLogger returns a logger writing JSON records to buf.
func newBufferLogger(t *testing.T, buf io.Writer) logger.Logger {
	t.Helper()

	lg, err := logger.New(logger.Config{
		Service: "grpcclient-test",
		Env:     logger.EnvDev,
		Format:  logger.FormatJSON,
		Writer:  buf,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	return lg
}
