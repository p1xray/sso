// Package grpcclient provides unified gRPC client connection construction on top
// of google.golang.org/grpc.
//
// New builds the connection from explicitly set options and waits for the target
// to become ready, retrying with exponential backoff, so a service does not
// start until the dependency is reachable or the attempts run out.
//
// Client delegates the RPC entry points of grpc.ClientConnInterface, so the
// value plugs into generated client constructors directly. Unwrap returns the
// underlying *grpc.ClientConn for everything else.
package grpcclient

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"github.com/p1xray/sso/backend/pkg/logger/sl"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
)

const (
	defaultConnectAttempts       = 10
	defaultConnectAttemptTimeout = time.Second
	defaultMaxBackoff            = 10 * time.Second
)

// Client provides access to the gRPC client connection.
type Client interface {
	// ClientConnInterface is embedded so generated client constructors
	// (NewAuthServiceClient and friends) accept the value directly.
	grpc.ClientConnInterface

	// Close tears down the connection and all underlying transports.
	Close() error

	// Unwrap returns the underlying *grpc.ClientConn.
	Unwrap() *grpc.ClientConn
}

type client struct {
	conn                  *grpc.ClientConn
	target                string
	creds                 credentials.TransportCredentials
	connectAttempts       int
	connectAttemptTimeout time.Duration
	maxBackoff            time.Duration
	grpcOptions           []grpc.DialOption
	logger                logger.Logger
}

// compile-time check that the concrete type satisfies the contract.
var _ Client = (*client)(nil)

// New builds a gRPC client connection from opts and waits until the target is
// ready. The target and an explicit transport security choice are required.
func New(ctx context.Context, opts ...Option) (*client, error) {
	c := &client{
		connectAttempts:       defaultConnectAttempts,
		connectAttemptTimeout: defaultConnectAttemptTimeout,
		maxBackoff:            defaultMaxBackoff,
	}

	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, fmt.Errorf("grpcclient: %w", err)
		}
	}

	if c.target == "" {
		return nil, fmt.Errorf("grpcclient: target is required")
	}
	if c.creds == nil {
		return nil, fmt.Errorf("grpcclient: transport security choice is required (use WithInsecure or WithTLS)")
	}

	// The package credentials go first, so a credentials option passed through
	// WithGRPCOptions wins.
	dialOptions := append(
		[]grpc.DialOption{grpc.WithTransportCredentials(c.creds)},
		c.grpcOptions...,
	)

	conn, err := grpc.NewClient(c.target, dialOptions...)
	if err != nil {
		return nil, fmt.Errorf("grpcclient: create connection: %w", err)
	}
	c.conn = conn

	if err = c.connectWithRetry(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("grpcclient: %w", err)
	}

	c.logConnected(ctx)

	return c, nil
}

// Invoke performs a unary RPC and returns after the response is received into
// reply.
func (c *client) Invoke(ctx context.Context, method string, args any, reply any, opts ...grpc.CallOption) error {
	return c.conn.Invoke(ctx, method, args, reply, opts...)
}

// NewStream begins a streaming RPC.
func (c *client) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.conn.NewStream(ctx, desc, method, opts...)
}

// Close tears down the connection and all underlying transports.
func (c *client) Close() error {
	return c.conn.Close()
}

// Unwrap returns the underlying *grpc.ClientConn.
func (c *client) Unwrap() *grpc.ClientConn {
	return c.conn
}

// connectWithRetry waits until the connection is ready, retrying with
// exponential backoff and jitter. Every attempt is bounded by maxBackoff, and
// the whole wait is cancellable through ctx.
func (c *client) connectWithRetry(ctx context.Context) error {
	delay := min(c.connectAttemptTimeout, c.maxBackoff)

	var err error
	for attempt := 1; attempt <= c.connectAttempts; attempt++ {
		// The package's cadence governs: a fresh attempt must not wait out the
		// channel's internal reconnect backoff left over from the previous one.
		c.conn.ResetConnectBackoff()

		attemptCtx, cancel := context.WithTimeout(ctx, c.maxBackoff)
		err = c.awaitReady(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}

		c.logFailedAttempt(ctx, attempt, err)

		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("connect: %w", ctxErr)
		}

		if attempt == c.connectAttempts {
			break
		}

		if err = sleep(ctx, jitter(delay)); err != nil {
			return fmt.Errorf("connect: %w", err)
		}

		if delay < c.maxBackoff {
			delay = min(delay*2, c.maxBackoff)
		}
	}

	return fmt.Errorf("connect after %d attempts: %w", c.connectAttempts, err)
}

// awaitReady blocks until the connection state is Ready or ctx is done. Connect
// in this grpc version is non-blocking, so the waiting is done by watching
// GetState/WaitForStateChange - the loop of grpc's own blocking dial - and
// kicking the channel out of idle.
func (c *client) awaitReady(ctx context.Context) error {
	for {
		state := c.conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Idle {
			c.conn.Connect()
		}
		if !c.conn.WaitForStateChange(ctx, state) {
			return ctx.Err()
		}
	}
}

// logConnected announces the target through the configured logger, if any.
func (c *client) logConnected(ctx context.Context) {
	if c.logger == nil {
		return
	}

	c.logger.Info(ctx, "grpcclient: connected", slog.String("target", c.target))
}

// logFailedAttempt warns through the configured logger, if any, that a connect
// attempt has failed.
func (c *client) logFailedAttempt(ctx context.Context, attempt int, err error) {
	if c.logger == nil {
		return
	}

	c.logger.Warn(ctx, "grpcclient: target is unreachable",
		slog.Int("attempt", attempt),
		slog.Int("total_attempts", c.connectAttempts),
		sl.Err(err),
	)
}

// jitter returns d scaled by a random factor in [0.5, 1), so a fleet of services
// starting together does not retry in lockstep.
func jitter(d time.Duration) time.Duration {
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// sleep waits for d and returns early when ctx is canceled.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
