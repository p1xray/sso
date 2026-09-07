package logger

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_NewEnv_SuccessfulKnownValues(t *testing.T) {
	tests := []struct {
		value string
		want  Env
	}{
		{
			value: "local",
			want:  EnvLocal,
		},
		{
			value: "dev",
			want:  EnvDev,
		},
		{
			value: "prod",
			want:  EnvProd,
		},
	}

	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := NewEnv(test.value)
			require.NoError(t, err)

			if got != test.want {
				t.Errorf("NewEnv(%q): want %q, got %q", test.value, test.want, got)
			}
		})
	}
}

func Test_NewEnv_FailsOnEmptyValue(t *testing.T) {
	env, err := NewEnv("")
	require.ErrorIs(t, err, ErrEmptyEnv)

	if env != "" {
		t.Errorf("want empty env on error, got %q", env)
	}
}

func Test_NewEnv_FailsOnUnknownValue(t *testing.T) {
	env, err := NewEnv("staging")
	require.ErrorIs(t, err, ErrInvalidEnv)

	if env != "" {
		t.Errorf("want empty env on error, got %q", env)
	}
}

func Test_String_ReturnsEnvAsString(t *testing.T) {
	if got := EnvDev.String(); got != "dev" {
		t.Errorf("want %q, got %q", "dev", got)
	}
}
