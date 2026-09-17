// Package grpcserver provides unified gRPC server construction on top of
// google.golang.org/grpc.
//
// New builds the server from explicitly set options, Start serves it in a
// background goroutine and Stop shuts it down: gracefully first, forcefully once
// the shutdown timeout expires. Notify reports the serve outcome: a nil value
// means the server stopped as requested, a non-nil one means a failure. The
// channel is closed after the outcome is delivered.
package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"github.com/p1xray/sso/backend/pkg/logger/sl"

	"google.golang.org/grpc"
)

const (
	network = "tcp"

	// defaultShutdownTimeout is how long Stop waits for the graceful shutdown before
	// forcing it.
	defaultShutdownTimeout = 10 * time.Second
)

// Server provides access to the gRPC server.
type Server interface {
	// Start starts serving in a background goroutine. A second call returns an
	// error.
	Start(ctx context.Context) error

	// Stop stops the server: gracefully first, forcefully once the shutdown timeout
	// expires. It is idempotent and always returns.
	Stop()

	// Notify returns the channel the serve outcome is sent to: nil for a graceful
	// stop, an error for a failure. The channel is closed after the outcome is
	// delivered.
	Notify() <-chan error

	// Registrar returns the target service registrations are attached to.
	Registrar() grpc.ServiceRegistrar

	// Unwrap returns the underlying *grpc.Server.
	Unwrap() *grpc.Server
}

type server struct {
	innerServer     *grpc.Server
	notify          chan error
	address         string
	shutdownTimeout time.Duration
	grpcOptions     []grpc.ServerOption
	logger          logger.Logger

	started       atomic.Bool
	stopRequested atomic.Bool
}

// compile-time check that the concrete type satisfies the contract.
var _ Server = (*server)(nil)

// New builds a gRPC server from opts. The listen address is required and every
// option is validated before the underlying gRPC server is created.
func New(opts ...Option) (*server, error) {
	s := &server{
		notify:          make(chan error, 1),
		shutdownTimeout: defaultShutdownTimeout,
	}

	for _, opt := range opts {
		if err := opt(s); err != nil {
			return nil, fmt.Errorf("grpcserver: %w", err)
		}
	}

	if s.address == "" {
		return nil, fmt.Errorf("grpcserver: address is required")
	}

	// The shutdown timeout is only honest if the connection handshake phase
	// cannot outlive it: grpc waits for handshaking connections on every
	// stop, holding them for its own 120s default. Appended first, so a
	// ConnectionTimeout passed through WithGRPCOptions wins.
	s.grpcOptions = append(
		[]grpc.ServerOption{grpc.ConnectionTimeout(s.shutdownTimeout)},
		s.grpcOptions...,
	)

	s.innerServer = grpc.NewServer(s.grpcOptions...)

	return s, nil
}

// Start starts serving in a background goroutine. It fails only when the server
// has already been started; the serve outcome arrives through Notify.
func (s *server) Start(ctx context.Context) error {
	isNotStarted := s.started.CompareAndSwap(false, true)
	if !isNotStarted {
		return fmt.Errorf("grpcserver: server is already started")
	}

	go s.serve(ctx)

	return nil
}

// Notify returns the channel the serve outcome is sent to: nil for a graceful
// stop, an error for a failure. The channel is closed after the outcome is
// delivered.
func (s *server) Notify() <-chan error {
	return s.notify
}

// Stop stops the server: gracefully first, forcefully once the shutdown
// timeout expires. It is idempotent and always returns.
func (s *server) Stop() {
	isStopNotRequested := s.stopRequested.CompareAndSwap(false, true)
	if !isStopNotRequested {
		return
	}

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		s.innerServer.GracefulStop()
	}()

	timer := time.NewTimer(s.shutdownTimeout)
	defer timer.Stop()

	select {
	case <-stopped:
	case <-timer.C:
		// Graceful shutdown did not finish in time: drop the remaining
		// connections and wait for the drain to observe it.
		s.innerServer.Stop()
		<-stopped
	}
}

// Registrar returns the target service registrations are attached to.
func (s *server) Registrar() grpc.ServiceRegistrar {
	return s.innerServer
}

// Unwrap returns the underlying *grpc.Server.
func (s *server) Unwrap() *grpc.Server {
	return s.innerServer
}

// serve listens on the address and serves until Stop, delivering the outcome
// through the notify channel and closing it.
func (s *server) serve(ctx context.Context) {
	defer close(s.notify)

	ln, err := net.Listen(network, s.address)
	if err != nil {
		err = fmt.Errorf("grpcserver: failed to listen on %s: %w", s.address, err)
		s.logError(ctx, "grpcserver: server start failed", err)
		s.notify <- err
		return
	}

	s.logStarted(ctx)

	serveErr := s.innerServer.Serve(ln)
	if s.stopRequested.Load() && errors.Is(serveErr, grpc.ErrServerStopped) {
		// Stop won the race before Serve started: the refused serve is the
		// requested shutdown, not a failure.
		serveErr = nil
	}

	if serveErr != nil {
		s.logError(ctx, "grpcserver: serve failed", serveErr)
	}

	s.notify <- serveErr
}

// logStarted announces the address through the configured logger, if any.
func (s *server) logStarted(ctx context.Context) {
	if s.logger == nil {
		return
	}

	s.logger.Info(ctx, "grpcserver: server started", slog.String("address", s.address))
}

// logError reports a failure through the configured logger, if any.
func (s *server) logError(ctx context.Context, msg string, err error) {
	if s.logger == nil {
		return
	}

	s.logger.Error(ctx, msg, sl.Err(err))
}
