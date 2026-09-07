package logger

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_NewLevel_SuccessfulExplicitValue(t *testing.T) {
	// NewLevel passes the value through as-is; parsing and validation
	// happen later in ParseToSlogLevel.
	for _, value := range []string{"debug", "INFO", "warn+2", "4"} {
		t.Run(value, func(t *testing.T) {
			got := NewLevel(value, EnvProd)

			if got != Level(value) {
				t.Errorf("NewLevel(%q): want %q, got %q", value, Level(value), got)
			}
		})
	}
}

func Test_NewLevel_DefaultsByEnv(t *testing.T) {
	tests := []struct {
		env  Env
		want Level
	}{
		{
			env:  EnvLocal,
			want: LevelDebug,
		},
		{
			env:  EnvDev,
			want: LevelDebug,
		},
		{
			env:  EnvProd,
			want: LevelInfo,
		},
	}

	for _, test := range tests {
		t.Run(test.env.String(), func(t *testing.T) {
			got := NewLevel("", test.env)

			if got != test.want {
				t.Errorf("NewLevel(\"\", %s): want %q, got %q", test.env, test.want, got)
			}
		})
	}
}

func Test_String_ReturnsLevelAsString(t *testing.T) {
	if got := LevelWarn.String(); got != "warn" {
		t.Errorf("want %q, got %q", "warn", got)
	}
}

func Test_ParseToSlogLevel_Successful(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{
			input: "debug",
			want:  slog.LevelDebug,
		},
		{
			input: "INFO",
			want:  slog.LevelInfo,
		},
		{
			input: "Warn",
			want:  slog.LevelWarn,
		},
		{
			input: "error",
			want:  slog.LevelError,
		},
		{
			input: "warning",
			want:  slog.LevelWarn,
		},
		{
			input: "info+2",
			want:  slog.Level(2),
		},
		{
			input: "error-4",
			want:  slog.LevelWarn,
		},
		{
			input: "4",
			want:  slog.LevelWarn,
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := Level(test.input).ParseToSlogLevel()
			require.NoError(t, err)

			if got != test.want {
				t.Errorf("ParseToSlogLevel(%q): want %v, got %v", test.input, test.want, got)
			}
		})
	}
}

func Test_ParseToSlogLevel_FailsOnInvalidValue(t *testing.T) {
	for _, input := range []string{"", "-4", "loud", "info+", "debug+loud", "d-e-b-u-g"} {
		t.Run("invalid_"+input, func(t *testing.T) {
			level, err := Level(input).ParseToSlogLevel()
			require.Error(t, err)

			if level != 0 {
				t.Errorf("want zero level on error, got %v", level)
			}
		})
	}
}
