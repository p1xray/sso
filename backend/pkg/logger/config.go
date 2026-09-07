package logger

import (
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"

	"github.com/p1xray/sso/backend/pkg/logger/handlers/ctx"
)

// StackTraceDisabled turns off stack trace capture.
const StackTraceDisabled = slog.Level(math.MaxInt32)

// SanitizeConfig controls masking of sensitive data. The zero value keeps
// sanitization on with the default deny-list, so a zero-value Config masks
// sensitive fields.
type SanitizeConfig struct {
	// Disabled turns all masking off, including the value scan.
	Disabled bool
	// ExtraKeys adds key names to mask, in addition to the default
	// deny-list. Matching is exact and case-insensitive.
	ExtraKeys []string
	// ExceptKeys removes key names from the default deny-list, e.g.
	// "code" for a service that logs non-sensitive codes under it.
	ExceptKeys []string
	// DisableValueScan skips the JWT/Bearer/Basic scan of string values.
	DisableValueScan bool
}

// Config configures logger.
type Config struct {
	// Service is the service name, required.
	Service string
	// Env is the deployment environment, required.
	Env Env
	// Version is the service version, optional.
	Version string
	// Instance identifies the running instance (host or pod name), optional.
	Instance string

	// Format selects the output representation of log records, defaults to
	// FormatAuto.
	Format Format
	// Level is a log level as a string, defaults by env: debug for local and dev, info for prod.
	Level Level

	// Writer writes log records, defaults to os.Stdout.
	Writer io.Writer
	// NoColor disables ANSI colors of the console format.
	NoColor bool

	// AddSource controls the source attribute. Nil means the default: true for
	// local, false otherwise.
	AddSource *bool
	// StackTraceLevel is the record level at and above which a stack trace attribute
	// is attached. Set StackTraceDisabled to turn stack trace capture off.
	StackTraceLevel slog.Level
	// Sanitize controls sensitive-data masking.
	Sanitize SanitizeConfig
	// ContextAttrs extracts attributes from the logging context.Context for every
	// record.
	ContextAttrs []ctx.ContextAttrFunc
}

// resolve validates the config, applies defaults and returns the effective
// options.
func (c Config) resolve() (options, error) {
	if c.Service == "" {
		return options{}, fmt.Errorf("logger: service name is required")
	}

	env, err := NewEnv(c.Env.String())
	if err != nil {
		return options{}, fmt.Errorf("logger: %w", err)
	}

	format, err := NewFormat(c.Format.String(), env)
	if err != nil {
		return options{}, fmt.Errorf("logger: %w", err)
	}

	level := NewLevel(c.Level.String(), env)
	levelNumber, err := level.ParseToSlogLevel()
	if err != nil {
		return options{}, fmt.Errorf("logger: %w", err)
	}

	addSource := env == EnvLocal
	if c.AddSource != nil {
		addSource = *c.AddSource
	}

	stackTraceLevel := c.StackTraceLevel
	if stackTraceLevel == 0 {
		stackTraceLevel = slog.LevelError
	}

	writer := c.Writer
	if writer == nil {
		writer = os.Stdout
	}

	contextExtractors := c.ContextAttrs
	if contextExtractors == nil {
		contextExtractors = []ctx.ContextAttrFunc{ctx.ExtractRequestID}
	}

	resolved := options{
		Service:           c.Service,
		Env:               env,
		Version:           c.Version,
		Instance:          c.Instance,
		Format:            format,
		Level:             levelNumber,
		Writer:            writer,
		NoColor:           c.NoColor,
		AddSource:         &addSource,
		StackTraceLevel:   stackTraceLevel,
		Sanitize:          c.Sanitize,
		ContextExtractors: contextExtractors,
	}

	return resolved, nil
}
