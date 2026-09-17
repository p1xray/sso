package httpserver

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
)

// Option customizes the server construction in New.
type Option func(*server) error

// WithAddress sets the address the HTTP server listens on, in host:port
// form. The address is required.
func WithAddress(address string) Option {
	return func(s *server) error {
		if address == "" {
			return fmt.Errorf("address is required")
		}

		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("address must be in host:port form, got %q", address)
		}

		s.address = address
		return nil
	}
}

// WithHandler sets the handler the requests are served by. The handler is
// required.
func WithHandler(handler http.Handler) Option {
	return func(s *server) error {
		if handler == nil {
			return fmt.Errorf("handler is required")
		}

		s.handler = handler
		return nil
	}
}

// WithShutdownTimeout sets how long Stop waits for the graceful shutdown
// before forcing it. Defaults to 10s.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(s *server) error {
		if timeout <= 0 {
			return fmt.Errorf("shutdown timeout must be positive, got %s", timeout)
		}

		s.shutdownTimeout = timeout
		return nil
	}
}

// WithHTTPServerConfig applies configure to the underlying *http.Server New
// builds: timeouts, TLS config, header limits and the like. The handler from
// WithHandler is assigned after configure, so it stays authoritative.
func WithHTTPServerConfig(configure func(srv *http.Server)) Option {
	return func(s *server) error {
		if configure == nil {
			return fmt.Errorf("configure function is required")
		}

		s.httpConfigure = configure
		return nil
	}
}

// WithLogger sets the logger for server lifecycle events: start, shutdown
// and failures. Nil (the default) keeps the lifecycle silent.
func WithLogger(lg logger.Logger) Option {
	return func(s *server) error {
		if lg == nil {
			return fmt.Errorf("lifecycle logger is required")
		}

		s.logger = lg
		return nil
	}
}
