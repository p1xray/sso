package grpcserver

import (
	"fmt"
	"net"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"google.golang.org/grpc"
)

// Option customizes the server construction in New.
type Option func(*server) error

// WithAddress sets the address the gRPC server listens on, in host:port
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

// WithShutdownTimeout sets how long Stop waits for the graceful shutdown
// before forcing it. It also caps the connection handshake timeout, so
// silent connections cannot hold the stop hostage past it. Defaults to 10s.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(s *server) error {
		if timeout <= 0 {
			return fmt.Errorf("shutdown timeout must be positive, got %s", timeout)
		}

		s.shutdownTimeout = timeout
		return nil
	}
}

// WithGRPCOptions appends options passed through to grpc.NewServer: TLS,
// keepalive, message size limits, interceptors and the like.
func WithGRPCOptions(opts ...grpc.ServerOption) Option {
	return func(s *server) error {
		s.grpcOptions = append(s.grpcOptions, opts...)
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
