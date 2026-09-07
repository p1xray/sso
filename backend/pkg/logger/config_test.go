package logger

import (
	"bytes"
	"log/slog"
	"testing"
)

func Test_Config_ResolveDefaults(t *testing.T) {
	tests := []struct {
		name          string
		env           Env
		format        Format
		level         Level
		addSource     *bool
		wantFormat    Format
		wantLevel     slog.Level
		wantAddSource bool
	}{
		{
			name:          "local defaults",
			env:           EnvLocal,
			format:        FormatAuto,
			wantFormat:    FormatConsole,
			wantLevel:     slog.LevelDebug,
			wantAddSource: true,
		},
		{
			name:          "dev defaults",
			env:           EnvDev,
			format:        FormatAuto,
			wantFormat:    FormatJSON,
			wantLevel:     slog.LevelDebug,
			wantAddSource: false,
		},
		{
			name:          "prod defaults",
			env:           EnvProd,
			format:        FormatAuto,
			wantFormat:    FormatJSON,
			wantLevel:     slog.LevelInfo,
			wantAddSource: false,
		},
		{
			name:          "explicit format wins on local",
			env:           EnvLocal,
			format:        FormatJSON,
			wantFormat:    FormatJSON,
			wantLevel:     slog.LevelDebug,
			wantAddSource: true,
		},
		{
			name:          "explicit level wins",
			env:           EnvProd,
			format:        FormatAuto,
			level:         LevelWarn,
			wantFormat:    FormatJSON,
			wantLevel:     slog.LevelWarn,
			wantAddSource: false,
		},
		{
			name:          "explicit addSource wins on prod",
			env:           EnvProd,
			format:        FormatAuto,
			addSource:     &[]bool{true}[0],
			wantFormat:    FormatJSON,
			wantLevel:     slog.LevelInfo,
			wantAddSource: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Config{
				Service: "auth",
				Env:     test.env,
				Format:  test.format,
				Level:   test.level,
				Writer:  &bytes.Buffer{},
			}
			if test.addSource != nil {
				config.AddSource = test.addSource
			}

			resolved, err := config.resolve()
			if err != nil {
				t.Fatalf("resolve returned error: %v", err)
			}
			if resolved.Format != test.wantFormat {
				t.Errorf("format: want %q, got %q", test.wantFormat, resolved.Format)
			}
			if resolved.Level != test.wantLevel {
				t.Errorf("level: want %v, got %v", test.wantLevel, resolved.Level)
			}
			if resolved.AddSource == nil || *resolved.AddSource != test.wantAddSource {
				t.Errorf("addSource: want %v, got %v", test.wantAddSource, resolved.AddSource)
			}
			if resolved.StackTraceLevel != slog.LevelError {
				t.Errorf("stackTraceLevel: want ERROR, got %v", resolved.StackTraceLevel)
			}
			if resolved.Writer == nil {
				t.Error("writer must be resolved to a non-nil value")
			}
		})
	}
}

func Test_Config_ResolveValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "empty service",
			config: Config{Env: EnvLocal},
		},
		{
			name:   "empty env",
			config: Config{Service: "auth"},
		},
		{
			name:   "invalid env",
			config: Config{Service: "auth", Env: Env("staging")},
		},
		{
			name:   "invalid format",
			config: Config{Service: "auth", Env: EnvLocal, Format: Format("xml")},
		},
		{
			name:   "invalid level",
			config: Config{Service: "auth", Env: EnvLocal, Level: Level("loud")},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.config.resolve(); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func Test_Config_ResolveKeepsStackTraceOverrides(t *testing.T) {
	config := Config{
		Service:         "auth",
		Env:             EnvProd,
		StackTraceLevel: slog.LevelWarn,
	}

	resolved, err := config.resolve()
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}
	if resolved.StackTraceLevel != slog.LevelWarn {
		t.Errorf("stackTraceLevel: want WARN, got %v", resolved.StackTraceLevel)
	}

	config.StackTraceLevel = StackTraceDisabled
	resolved, err = config.resolve()
	if err != nil {
		t.Fatalf("resolve returned error: %v", err)
	}
	if resolved.StackTraceLevel != StackTraceDisabled {
		t.Errorf("stackTraceLevel: want StackTraceDisabled, got %v", resolved.StackTraceLevel)
	}
}
