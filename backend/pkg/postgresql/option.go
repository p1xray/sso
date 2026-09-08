package postgresql

import (
	"fmt"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
)

// Option customizes the pool construction in New.
type Option func(*postgres) error

// ConnAttempts sets how many times New pings the database before giving up.
// Defaults to 10.
func ConnAttempts(attempts int) Option {
	return func(p *postgres) error {
		if attempts < 1 {
			return fmt.Errorf("connection attempts must be at least 1, got %d", attempts)
		}

		p.connAttempts = attempts
		return nil
	}
}

// ConnAttemptTimeout sets the initial delay between ping attempts. It doubles
// on every attempt up to MaxBackoff. Defaults to 1s.
func ConnAttemptTimeout(timeout time.Duration) Option {
	return func(p *postgres) error {
		if timeout <= 0 {
			return fmt.Errorf("connection attempt timeout must be positive, got %s", timeout)
		}

		p.connAttemptTimeout = timeout
		return nil
	}
}

// MaxBackoff caps both the delay between ping attempts and each attempt's
// duration. Defaults to 10s.
func MaxBackoff(backoff time.Duration) Option {
	return func(p *postgres) error {
		if backoff <= 0 {
			return fmt.Errorf("connection backoff must be positive, got %s", backoff)
		}

		p.maxBackoff = backoff
		return nil
	}
}

// LogQueries logs every query the pool executes: the SQL with its args at
// info level, failures at error level.
func LogQueries(logger logger.Logger) Option {
	return func(p *postgres) error {
		if logger == nil {
			return fmt.Errorf("query logger is required")
		}

		p.poolConfig.ConnConfig.Tracer = &loggerTracer{logger: logger}
		return nil
	}
}

// LogPingAttempts sets the logger for ping attempts. Nil (the default) keeps the attempts silent.
func LogPingAttempts(logger logger.Logger) Option {
	return func(p *postgres) error {
		p.pingAttemptsLogger = logger
		return nil
	}
}
