package grpcserver

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"google.golang.org/grpc"
)

func Test_WithAddress_SetsValue(t *testing.T) {
	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "explicit host",
			address: "127.0.0.1:9090",
		},
		{
			name:    "wildcard host",
			address: ":9090",
		},
		{
			name:    "ephemeral port",
			address: "127.0.0.1:0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{}

			if err := WithAddress(tt.address)(s); err != nil {
				t.Fatalf("WithAddress() error = %v", err)
			}
			if s.address != tt.address {
				t.Errorf("address = %q, want %q", s.address, tt.address)
			}
		})
	}
}

func Test_WithAddress_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr string
	}{
		{
			name:    "empty",
			address: "",
			wantErr: "address is required",
		},
		{
			name:    "missing port",
			address: "127.0.0.1",
			wantErr: "address must be in host:port form",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &server{}

			err := WithAddress(tt.address)(s)
			if err == nil {
				t.Fatal("WithAddress() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("WithAddress() error = %q, want it to contain %q", err, tt.wantErr)
			}
			if s.address != "" {
				t.Errorf("address = %q, want it unchanged", s.address)
			}
		})
	}
}

func Test_WithShutdownTimeout_SetsValue(t *testing.T) {
	s := &server{}

	if err := WithShutdownTimeout(3 * time.Second)(s); err != nil {
		t.Fatalf("WithShutdownTimeout() error = %v", err)
	}
	if s.shutdownTimeout != 3*time.Second {
		t.Errorf("shutdownTimeout = %s, want %s", s.shutdownTimeout, 3*time.Second)
	}
}

func Test_WithShutdownTimeout_RejectsNonPositive(t *testing.T) {
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
			s := &server{shutdownTimeout: time.Second}

			err := WithShutdownTimeout(tt.timeout)(s)
			if err == nil {
				t.Fatal("WithShutdownTimeout() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "shutdown timeout must be positive") {
				t.Errorf("WithShutdownTimeout() error = %q, want the validation message", err)
			}
			if s.shutdownTimeout != time.Second {
				t.Errorf("shutdownTimeout = %s, want it unchanged", s.shutdownTimeout)
			}
		})
	}
}

func Test_WithGRPCOptions_Appends(t *testing.T) {
	s := &server{}

	if err := WithGRPCOptions(grpc.MaxRecvMsgSize(1024))(s); err != nil {
		t.Fatalf("WithGRPCOptions() error = %v", err)
	}
	if err := WithGRPCOptions(grpc.MaxRecvMsgSize(2048), grpc.MaxSendMsgSize(4096))(s); err != nil {
		t.Fatalf("WithGRPCOptions() error = %v", err)
	}

	if len(s.grpcOptions) != 3 {
		t.Errorf("grpcOptions count = %d, want 3", len(s.grpcOptions))
	}
}

func Test_WithLogger_SetsValue(t *testing.T) {
	lg := newBufferLogger(t, io.Discard)
	s := &server{}

	if err := WithLogger(lg)(s); err != nil {
		t.Fatalf("WithLogger() error = %v", err)
	}
	if s.logger != lg {
		t.Error("logger does not match the configured one")
	}
}

func Test_WithLogger_RejectsNil(t *testing.T) {
	s := &server{}

	err := WithLogger(nil)(s)
	if err == nil {
		t.Fatal("WithLogger() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "lifecycle logger is required") {
		t.Errorf("WithLogger() error = %q, want the validation message", err)
	}
	if s.logger != nil {
		t.Error("logger = non-nil, want it unset")
	}
}

func Test_New_RequiresAddress(t *testing.T) {
	_, err := New()
	if err == nil {
		t.Fatal("New() error = nil, want address error")
	}
	if !strings.Contains(err.Error(), "grpcserver: address is required") {
		t.Errorf("New() error = %q, want the address error wrapped by the package", err)
	}
}

func Test_New_InvalidOption(t *testing.T) {
	tests := []struct {
		name    string
		option  Option
		wantErr string
	}{
		{
			name:    "empty address",
			option:  WithAddress(""),
			wantErr: "address is required",
		},
		{
			name:    "address without a port",
			option:  WithAddress("127.0.0.1"),
			wantErr: "address must be in host:port form",
		},
		{
			name:    "non-positive shutdown timeout",
			option:  WithShutdownTimeout(-time.Second),
			wantErr: "shutdown timeout must be positive",
		},
		{
			name:    "nil lifecycle logger",
			option:  WithLogger(nil),
			wantErr: "lifecycle logger is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.option)
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
		Service: "grpcserver-test",
		Env:     logger.EnvDev,
		Format:  logger.FormatJSON,
		Writer:  buf,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	return lg
}
