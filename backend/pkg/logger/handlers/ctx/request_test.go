package ctx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func newRequestID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func Test_RequestIDRoundTrip(t *testing.T) {
	ctx := WithRequestID(context.Background(), "abc-123")
	if got := RequestIDFromContext(ctx); got != "abc-123" {
		t.Errorf("want %q, got %q", "abc-123", got)
	}
}

func Test_RequestIDAbsent(t *testing.T) {
	if got := RequestIDFromContext(context.Background()); got != "" {
		t.Errorf("want empty request id, got %q", got)
	}
}

func Test_ExtractRequestID(t *testing.T) {
	if attrs := ExtractRequestID(context.Background()); attrs != nil {
		t.Errorf("want nil attrs without request id, got %v", attrs)
	}

	ctx := WithRequestID(context.Background(), "req-42")
	attrs := ExtractRequestID(ctx)
	if len(attrs) != 1 {
		t.Fatalf("want 1 attr, got %d", len(attrs))
	}
	if attrs[0].Key != "request_id" || attrs[0].Value.String() != "req-42" {
		t.Errorf("unexpected attr: %v", attrs[0])
	}
}

func Test_newRequestID(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id, err := newRequestID()
		if err != nil {
			t.Fatalf("newRequestID returned error: %v", err)
		}
		if len(id) != 32 {
			t.Fatalf("want 32 hex chars, got %d: %q", len(id), id)
		}
		if _, err := hex.DecodeString(id); err != nil {
			t.Fatalf("want valid hex, got %q: %v", id, err)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate request id: %q", id)
		}
		seen[id] = struct{}{}
	}
}
