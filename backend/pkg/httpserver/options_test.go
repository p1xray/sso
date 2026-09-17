package httpserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
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

func Test_WithHandler_SetsValue(t *testing.T) {
	handler := stubHandler{}
	s := &server{}

	if err := WithHandler(handler)(s); err != nil {
		t.Fatalf("WithHandler() error = %v", err)
	}
	if s.handler != handler {
		t.Error("handler does not match the configured one")
	}
}

// stubHandler is a comparable handler, so tests can check the exact one was set.
type stubHandler struct{}

func (stubHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func Test_WithHandler_RejectsNil(t *testing.T) {
	s := &server{}

	err := WithHandler(nil)(s)
	if err == nil {
		t.Fatal("WithHandler() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "handler is required") {
		t.Errorf("WithHandler() error = %q, want the validation message", err)
	}
	if s.handler != nil {
		t.Error("handler = non-nil, want it unset")
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

func Test_WithHTTPServerConfig_SetsValue(t *testing.T) {
	configure := func(srv *http.Server) {}
	s := &server{}

	if err := WithHTTPServerConfig(configure)(s); err != nil {
		t.Fatalf("WithHTTPServerConfig() error = %v", err)
	}
	if s.httpConfigure == nil {
		t.Error("httpConfigure = nil, want it set")
	}
}

func Test_WithHTTPServerConfig_RejectsNil(t *testing.T) {
	s := &server{}

	err := WithHTTPServerConfig(nil)(s)
	if err == nil {
		t.Fatal("WithHTTPServerConfig() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "configure function is required") {
		t.Errorf("WithHTTPServerConfig() error = %q, want the validation message", err)
	}
	if s.httpConfigure != nil {
		t.Error("httpConfigure = non-nil, want it unset")
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
	_, err := New(WithHandler(http.NotFoundHandler()))
	if err == nil {
		t.Fatal("New() error = nil, want address error")
	}
	if !strings.Contains(err.Error(), "httpserver: address is required") {
		t.Errorf("New() error = %q, want the address error wrapped by the package", err)
	}
}

func Test_New_RequiresHandler(t *testing.T) {
	_, err := New(WithAddress("127.0.0.1:9090"))
	if err == nil {
		t.Fatal("New() error = nil, want handler error")
	}
	if !strings.Contains(err.Error(), "httpserver: handler is required") {
		t.Errorf("New() error = %q, want the handler error wrapped by the package", err)
	}
}

func Test_New_AppliesHTTPServerConfig(t *testing.T) {
	srv, err := New(
		WithAddress("127.0.0.1:9090"),
		WithHandler(http.NotFoundHandler()),
		WithHTTPServerConfig(func(s *http.Server) {
			s.ReadHeaderTimeout = 5 * time.Second
		}),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if srv.Unwrap().ReadHeaderTimeout != 5*time.Second {
		t.Errorf(
			"ReadHeaderTimeout = %s, want the configured 5s",
			srv.Unwrap().ReadHeaderTimeout,
		)
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
			name:    "nil handler",
			option:  WithHandler(nil),
			wantErr: "handler is required",
		},
		{
			name:    "non-positive shutdown timeout",
			option:  WithShutdownTimeout(-time.Second),
			wantErr: "shutdown timeout must be positive",
		},
		{
			name:    "nil server configurator",
			option:  WithHTTPServerConfig(nil),
			wantErr: "configure function is required",
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
		Service: "httpserver-test",
		Env:     logger.EnvDev,
		Format:  logger.FormatJSON,
		Writer:  buf,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	return lg
}
