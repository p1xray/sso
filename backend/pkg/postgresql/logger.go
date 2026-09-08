package postgresql

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/p1xray/sso/backend/pkg/logger"
)

// loggerTracer feeds every query pgx executes to the logger. Installed by
// LogQueries as the pool connection's tracer.
type loggerTracer struct {
	logger logger.Logger
}

// TraceQueryStart logs the query about to be executed with its SQL and args.
func (l *loggerTracer) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	l.logger.Info(ctx,
		"executing query",
		slog.String("sql", data.SQL),
		slog.Any("args", data.Args),
	)
	return ctx
}

// TraceQueryEnd logs a failed query.
func (l *loggerTracer) TraceQueryEnd(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryEndData,
) {
	if data.Err != nil {
		l.logger.Error(ctx, "query failed", slog.Any("error", data.Err))
	}
}
