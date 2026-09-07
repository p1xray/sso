package sl

import (
	"errors"
	"testing"
)

func Test_Err_ReturnsErrorAttr(t *testing.T) {
	attr := Err(errors.New("boom"))

	if attr.Key != "error" {
		t.Errorf("want key %q, got %q", "error", attr.Key)
	}
	if got := attr.Value.String(); got != "boom" {
		t.Errorf("want value %q, got %q", "boom", got)
	}
}

func Test_Strings_JoinsValuesIntoOneAttr(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{
			name:   "several values",
			values: []string{"a", "b", "c"},
			want:   "a b c",
		},
		{
			name:   "one value",
			values: []string{"a"},
			want:   "a",
		},
		{
			name:   "empty slice",
			values: []string{},
			want:   "",
		},
		{
			name:   "nil slice",
			values: nil,
			want:   "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			attr := Strings("ids", test.values)

			if attr.Key != "ids" {
				t.Errorf("want key %q, got %q", "ids", attr.Key)
			}
			if got := attr.Value.String(); got != test.want {
				t.Errorf("want value %q, got %q", test.want, got)
			}
		})
	}
}
