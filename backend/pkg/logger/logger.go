package logger

import (
	"log/slog"
	"os"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/slogpretty"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func SetupLogger(environment string) *slog.Logger {
	var log *slog.Logger

	switch environment {
	case envLocal:
		log = setupConsolePrettyLogger(slog.LevelDebug)
	case envDev:
		log = setupConsoleDefaultLogger(slog.LevelDebug)
	case envProd:
		log = setupConsoleDefaultLogger(slog.LevelInfo)
	default:
		log = slog.Default()
	}

	return log
}

func setupConsolePrettyLogger(level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slogpretty.NewPrettyHandler(opts, os.Stdout, slogpretty.WithColor())

	return slog.New(handler)
}

func setupConsoleDefaultLogger(level slog.Level) *slog.Logger {
	log := slog.New(
		slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}),
	)

	return log
}
