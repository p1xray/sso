package grpcclient

import (
	"crypto/tls"
	"fmt"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Option customizes the client construction in New.
type Option func(*client) error

// WithTarget sets the address of the gRPC server to connect to, in any form grpc
// accepts: host:port, dns:///host:port, unix:///path and the like.
func WithTarget(target string) Option {
	return func(c *client) error {
		if target == "" {
			return fmt.Errorf("target is required")
		}

		c.target = target
		return nil
	}
}

// WithInsecure disables transport security: no encryption and no server
// authentication. For local development and trusted internal networks.
// Mutually exclusive with WithTLS.
func WithInsecure() Option {
	return func(c *client) error {
		if c.creds != nil {
			return fmt.Errorf("transport security is already configured")
		}

		c.creds = insecure.NewCredentials()
		return nil
	}
}

// WithTLS sets TLS as the transport security with the given config.
// Mutually exclusive with WithInsecure.
func WithTLS(config *tls.Config) Option {
	return func(c *client) error {
		if config == nil {
			return fmt.Errorf("TLS config is required")
		}
		if c.creds != nil {
			return fmt.Errorf("transport security is already configured")
		}

		c.creds = credentials.NewTLS(config)
		return nil
	}
}

// WithConnectAttempts sets how many times New waits for the target to become
// ready before giving up. Defaults to 10.
func WithConnectAttempts(attempts int) Option {
	return func(c *client) error {
		if attempts < 1 {
			return fmt.Errorf("connection attempts must be at least 1, got %d", attempts)
		}

		c.connectAttempts = attempts
		return nil
	}
}

// WithConnectAttemptTimeout sets the initial delay between connect attempts,
// doubling up to WithMaxBackoff. Defaults to 1s.
func WithConnectAttemptTimeout(timeout time.Duration) Option {
	return func(c *client) error {
		if timeout <= 0 {
			return fmt.Errorf("connection attempt timeout must be positive, got %s", timeout)
		}

		c.connectAttemptTimeout = timeout
		return nil
	}
}

// WithMaxBackoff sets the ceiling for both the delay between connect attempts
// and the duration of a single attempt. Defaults to 10s.
func WithMaxBackoff(backoff time.Duration) Option {
	return func(c *client) error {
		if backoff <= 0 {
			return fmt.Errorf("connection backoff must be positive, got %s", backoff)
		}

		c.maxBackoff = backoff
		return nil
	}
}

// WithLogger sets the logger for client lifecycle events: connection
// established, unreachable target attempts. Nil (the default) keeps the
// lifecycle silent.
func WithLogger(lg logger.Logger) Option {
	return func(c *client) error {
		if lg == nil {
			return fmt.Errorf("lifecycle logger is required")
		}

		c.logger = lg
		return nil
	}
}

// WithGRPCOptions appends options passed through to grpc.NewClient: TLS,
// keepalive, message size limits, interceptors and the like.
func WithGRPCOptions(opts ...grpc.DialOption) Option {
	return func(c *client) error {
		c.grpcOptions = append(c.grpcOptions, opts...)
		return nil
	}
}
