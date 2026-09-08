// Package postgresql provides unified PostgreSQL connection pooling on top of
// github.com/jackc/pgx/v5/pgxpool.
//
// New parses the connection URL, applies the explicitly set options and pings
// the database with exponential backoff, so a service does not start until
// PostgreSQL is reachable or the attempts run out.
//
// Postgres delegates the common pool operations. Unwrap returns the underlying
// pool for everything else.
package postgresql

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/p1xray/sso/backend/pkg/logger"
)

const (
	_defaultConnAttempts       = 10
	_defaultConnAttemptTimeout = time.Second
	_defaultMaxBackoff         = 10 * time.Second
)

// Postgres provides access to the PostgreSQL connection pool.
type Postgres interface {
	// Exec executes sql with no rows expected back.
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)

	// Query executes sql and returns the rows.
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)

	// QueryRow executes sql and scans the first result row, if any.
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row

	// Begin starts a transaction.
	Begin(ctx context.Context) (pgx.Tx, error)

	// BeginTx starts a transaction with the given options.
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)

	// BeginFunc starts a transaction and calls fn with it: a nil return commits,
	// any error rolls back and is returned.
	BeginFunc(ctx context.Context, fn func(pgx.Tx) error) error

	// CopyFrom copies rows from rowSrc into the table in a single COPY operation.
	CopyFrom(
		ctx context.Context,
		tableName pgx.Identifier,
		columnNames []string,
		rowSrc pgx.CopyFromSource,
	) (int64, error)

	// Ping verifies the database is reachable.
	Ping(ctx context.Context) error

	// Stat returns the pool statistics.
	Stat() *pgxpool.Stat

	// Close closes the pool and all its connections.
	Close()
}

type postgres struct {
	connAttempts       int
	connAttemptTimeout time.Duration
	maxBackoff         time.Duration
	pingAttemptsLogger logger.Logger

	poolConfig *pgxpool.Config
	pool       *pgxpool.Pool
}

// compile-time check that the concrete type satisfies the contract.
var _ Postgres = (*postgres)(nil)

// New creates a connection pool and verifies the database is reachable, retrying
// with backoff.
func New(ctx context.Context, connString string, opts ...Option) (*postgres, error) {
	poolConfig, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("postgresql: parse config: %w", err)
	}

	p := &postgres{
		connAttempts:       _defaultConnAttempts,
		connAttemptTimeout: _defaultConnAttemptTimeout,
		maxBackoff:         _defaultMaxBackoff,
		poolConfig:         poolConfig,
	}

	for _, opt := range opts {
		if err = opt(p); err != nil {
			return nil, fmt.Errorf("postgresql: %w", err)
		}
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("postgresql: create pool: %w", err)
	}

	p.pool = pool
	if err = p.pingWithRetry(ctx); err != nil {
		p.Close()
		return nil, fmt.Errorf("postgresql: %w", err)
	}

	return p, nil
}

// Exec executes sql with no rows expected back.
func (p *postgres) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

// Query executes sql and returns the rows.
func (p *postgres) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow executes sql and scans the first result row, if any.
func (p *postgres) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// Begin starts a transaction.
func (p *postgres) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(ctx)
}

// BeginTx starts a transaction with the given options.
func (p *postgres) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	return p.pool.BeginTx(ctx, txOptions)
}

// BeginFunc starts a transaction and calls fn with it: a nil return commits,
// any error rolls back and is returned.
func (p *postgres) BeginFunc(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, p.pool, fn)
}

// SendBatch sends b in a single round trip and returns the results.
func (p *postgres) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	return p.pool.SendBatch(ctx, b)
}

// CopyFrom copies rows from rowSrc into the table in a single COPY operation.
func (p *postgres) CopyFrom(
	ctx context.Context,
	tableName pgx.Identifier,
	columnNames []string,
	rowSrc pgx.CopyFromSource,
) (int64, error) {
	return p.pool.CopyFrom(ctx, tableName, columnNames, rowSrc)
}

// Ping verifies the database is reachable.
func (p *postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Stat returns the pool statistics.
func (p *postgres) Stat() *pgxpool.Stat {
	return p.pool.Stat()
}

// Close closes the pool and all its connections.
func (p *postgres) Close() {
	p.pool.Close()
}

// Unwrap returns the underlying *pgxpool.Pool.
func (p *postgres) Unwrap() *pgxpool.Pool {
	return p.pool
}

// pingWithRetry verifies the database is reachable, retrying with exponential
// backoff and jitter. Every attempt is bounded by maxBackoff, and the whole
// wait is cancellable through ctx.
func (p *postgres) pingWithRetry(ctx context.Context) error {
	delay := min(p.connAttemptTimeout, p.maxBackoff)

	var err error
	for attempt := 1; attempt <= p.connAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, p.maxBackoff)
		err = p.pool.Ping(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}

		p.logFailedAttempt(ctx, attempt, err)

		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("ping: %w", ctxErr)
		}

		if attempt == p.connAttempts {
			break
		}

		if err = sleep(ctx, jitter(delay)); err != nil {
			return fmt.Errorf("ping: %w", err)
		}

		if delay < p.maxBackoff {
			delay = min(delay*2, p.maxBackoff)
		}
	}

	return fmt.Errorf("ping after %d attempts: %w", p.connAttempts, err)
}

// logFailedAttempt warns through the configured logger, if any, that a ping
// attempt has failed.
func (p *postgres) logFailedAttempt(ctx context.Context, attempt int, err error) {
	if p.pingAttemptsLogger == nil {
		return
	}

	p.pingAttemptsLogger.Warn(ctx, "postgresql: database is unreachable",
		slog.Int("attempt", attempt),
		slog.Int("total_attempts", p.connAttempts),
		slog.Any("error", err),
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
