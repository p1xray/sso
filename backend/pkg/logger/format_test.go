package logger

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_NewFormat_SuccessfulResolve(t *testing.T) {
	tests := []struct {
		name  string
		value string
		env   Env
		want  Format
	}{
		{
			name:  "empty resolves to console on local",
			value: "",
			env:   EnvLocal,
			want:  FormatConsole,
		},
		{
			name:  "empty resolves to json on dev",
			value: "",
			env:   EnvDev,
			want:  FormatJSON,
		},
		{
			name:  "empty resolves to json on prod",
			value: "",
			env:   EnvProd,
			want:  FormatJSON,
		},
		{
			name:  "auto resolves to console on local",
			value: "auto",
			env:   EnvLocal,
			want:  FormatConsole,
		},
		{
			name:  "auto resolves to json on prod",
			value: "auto",
			env:   EnvProd,
			want:  FormatJSON,
		},
		{
			name:  "explicit json wins on local",
			value: "json",
			env:   EnvLocal,
			want:  FormatJSON,
		},
		{
			name:  "explicit console wins on prod",
			value: "console",
			env:   EnvProd,
			want:  FormatConsole,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewFormat(test.value, test.env)
			require.NoError(t, err)

			if got != test.want {
				t.Errorf("NewFormat(%q, %s): want %q, got %q", test.value, test.env, test.want, got)
			}
		})
	}
}

func Test_NewFormat_FailsOnUnknownValue(t *testing.T) {
	for _, value := range []string{"xml", "JSON", "pretty"} {
		t.Run(value, func(t *testing.T) {
			format, err := NewFormat(value, EnvLocal)
			require.ErrorIs(t, err, ErrInvalidFormat)

			if format != "" {
				t.Errorf("want empty format on error, got %q", format)
			}
		})
	}
}

func Test_String_ReturnsFormatAsString(t *testing.T) {
	if got := FormatJSON.String(); got != "json" {
		t.Errorf("want %q, got %q", "json", got)
	}
}
