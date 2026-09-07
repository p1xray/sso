package color

import (
	"testing"
)

// both strategies satisfy Colorizer
var _ Colorizer = WithColorize
var _ Colorizer = WithoutColorize

func Test_WithColorize_WrapsValueInColorCode(t *testing.T) {
	tests := []struct {
		name string
		code int
		want string
	}{
		{
			name: "cyan",
			code: Cyan,
			want: "\033[36mvalue\033[0m",
		},
		{
			name: "light red",
			code: LightRed,
			want: "\033[91mvalue\033[0m",
		},
		{
			name: "dark gray",
			code: DarkGray,
			want: "\033[90mvalue\033[0m",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := WithColorize(test.code, "value")

			if got != test.want {
				t.Errorf("WithColorize(%d): want %q, got %q", test.code, test.want, got)
			}
		})
	}
}

func Test_WithoutColorize_ReturnsValueUnchanged(t *testing.T) {
	if got := WithoutColorize(Cyan, "value"); got != "value" {
		t.Errorf("want %q, got %q", "value", got)
	}
}
