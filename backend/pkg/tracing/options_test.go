package tracing

import (
	"context"
	"crypto/tls"
	"io"
	"strings"
	"testing"
	"time"
)

func Test_WithEndpoint_SetsValue(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{
			name:     "localhost",
			endpoint: "localhost:4317",
		},
		{
			name:     "IPv4",
			endpoint: "127.0.0.1:4317",
		},
		{
			name:     "collector host",
			endpoint: "otel-collector:4317",
		},
		{
			name:     "bracketed IPv6",
			endpoint: "[::1]:4317",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{}

			if err := WithEndpoint(tt.endpoint)(p); err != nil {
				t.Fatalf("WithEndpoint() error = %v", err)
			}
			if p.endpoint != tt.endpoint {
				t.Errorf("endpoint = %q, want %q", p.endpoint, tt.endpoint)
			}
		})
	}
}

func Test_WithEndpoint_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{
			name:     "empty",
			endpoint: "",
			wantErr:  "endpoint is required",
		},
		{
			name:     "missing port",
			endpoint: "localhost",
			wantErr:  "host:port",
		},
		{
			name:     "extra colon",
			endpoint: "localhost:4317:extra",
			wantErr:  "host:port",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{endpoint: "127.0.0.1:4317"}

			err := WithEndpoint(tt.endpoint)(p)
			if err == nil {
				t.Fatal("WithEndpoint() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("WithEndpoint() error = %q, want it to contain %q", err, tt.wantErr)
			}
			if p.endpoint != "127.0.0.1:4317" {
				t.Errorf("endpoint = %q, want it unchanged", p.endpoint)
			}
		})
	}
}

func Test_WithInsecure_SetsValue(t *testing.T) {
	p := &provider{}

	if err := WithInsecure()(p); err != nil {
		t.Fatalf("WithInsecure() error = %v", err)
	}
	if !p.insecure {
		t.Error("insecure = false, want true")
	}
}

func Test_WithInsecure_RejectsAfterTLS(t *testing.T) {
	p := &provider{tlsConfig: &tls.Config{}}

	err := WithInsecure()(p)
	if err == nil {
		t.Fatal("WithInsecure() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "transport security is already configured") {
		t.Errorf("WithInsecure() error = %q, want the validation message", err)
	}
	if p.insecure {
		t.Error("insecure = true, want the TLS choice to stay")
	}
}

func Test_WithTLS_SetsValue(t *testing.T) {
	p := &provider{}
	config := &tls.Config{}

	if err := WithTLS(config)(p); err != nil {
		t.Fatalf("WithTLS() error = %v", err)
	}
	if p.tlsConfig != config {
		t.Error("tlsConfig does not match the configured one")
	}
}

func Test_WithTLS_RejectsNil(t *testing.T) {
	p := &provider{}

	err := WithTLS(nil)(p)
	if err == nil {
		t.Fatal("WithTLS() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "TLS config is required") {
		t.Errorf("WithTLS() error = %q, want the validation message", err)
	}
	if p.tlsConfig != nil {
		t.Error("tlsConfig = non-nil, want it unset")
	}
}

func Test_WithTLS_RejectsAfterInsecure(t *testing.T) {
	p := &provider{insecure: true}

	err := WithTLS(&tls.Config{})(p)
	if err == nil {
		t.Fatal("WithTLS() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "transport security is already configured") {
		t.Errorf("WithTLS() error = %q, want the validation message", err)
	}
	if p.tlsConfig != nil {
		t.Error("tlsConfig = non-nil, want the insecure choice to stay")
	}
}

func Test_WithHeaders_SetsCopy(t *testing.T) {
	headers := map[string]string{"X-Scope-OrgID": "sso"}

	p := &provider{}
	if err := WithHeaders(headers)(p); err != nil {
		t.Fatalf("WithHeaders() error = %v", err)
	}

	// Mutating the caller's map must not leak into the provider.
	headers["X-Scope-OrgID"] = "other"

	if got := p.headers["X-Scope-OrgID"]; got != "sso" {
		t.Errorf("headers[X-Scope-OrgID] = %q, want the copied value %q", got, "sso")
	}
}

func Test_WithHeaders_RejectsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{
			name:    "nil map",
			headers: nil,
		},
		{
			name:    "empty map",
			headers: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{}

			err := WithHeaders(tt.headers)(p)
			if err == nil {
				t.Fatal("WithHeaders() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "at least one header is required") {
				t.Errorf("WithHeaders() error = %q, want the validation message", err)
			}
			if p.headers != nil {
				t.Error("headers = non-nil, want it unset")
			}
		})
	}
}

func Test_WithServiceName_SetsValue(t *testing.T) {
	p := &provider{}

	if err := WithServiceName("auth")(p); err != nil {
		t.Fatalf("WithServiceName() error = %v", err)
	}
	if p.serviceName != "auth" {
		t.Errorf("serviceName = %q, want %q", p.serviceName, "auth")
	}
}

func Test_WithServiceName_RejectsEmpty(t *testing.T) {
	p := &provider{serviceName: "auth"}

	err := WithServiceName("")(p)
	if err == nil {
		t.Fatal("WithServiceName() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "service name is required") {
		t.Errorf("WithServiceName() error = %q, want the validation message", err)
	}
	if p.serviceName != "auth" {
		t.Errorf("serviceName = %q, want it unchanged", p.serviceName)
	}
}

func Test_WithServiceVersion_SetsValue(t *testing.T) {
	p := &provider{}

	if err := WithServiceVersion("v1.2.3")(p); err != nil {
		t.Fatalf("WithServiceVersion() error = %v", err)
	}
	if p.serviceVersion != "v1.2.3" {
		t.Errorf("serviceVersion = %q, want %q", p.serviceVersion, "v1.2.3")
	}
}

func Test_WithServiceVersion_RejectsEmpty(t *testing.T) {
	p := &provider{serviceVersion: "v1.2.3"}

	err := WithServiceVersion("")(p)
	if err == nil {
		t.Fatal("WithServiceVersion() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "service version is required") {
		t.Errorf("WithServiceVersion() error = %q, want the validation message", err)
	}
	if p.serviceVersion != "v1.2.3" {
		t.Errorf("serviceVersion = %q, want it unchanged", p.serviceVersion)
	}
}

func Test_WithEnvironment_SetsValue(t *testing.T) {
	p := &provider{}

	if err := WithEnvironment("staging")(p); err != nil {
		t.Fatalf("WithEnvironment() error = %v", err)
	}
	if p.environment != "staging" {
		t.Errorf("environment = %q, want %q", p.environment, "staging")
	}
}

func Test_WithEnvironment_RejectsEmpty(t *testing.T) {
	p := &provider{environment: "staging"}

	err := WithEnvironment("")(p)
	if err == nil {
		t.Fatal("WithEnvironment() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "environment is required") {
		t.Errorf("WithEnvironment() error = %q, want the validation message", err)
	}
	if p.environment != "staging" {
		t.Errorf("environment = %q, want it unchanged", p.environment)
	}
}

func Test_WithSamplingRatio_SetsValue(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
	}{
		{
			name:  "nothing",
			ratio: 0,
		},
		{
			name:  "half",
			ratio: 0.5,
		},
		{
			name:  "everything",
			ratio: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{}

			if err := WithSamplingRatio(tt.ratio)(p); err != nil {
				t.Fatalf("WithSamplingRatio() error = %v", err)
			}
			if p.samplingRatio != tt.ratio {
				t.Errorf("samplingRatio = %g, want %g", p.samplingRatio, tt.ratio)
			}
		})
	}
}

func Test_WithSamplingRatio_RejectsOutOfRange(t *testing.T) {
	tests := []struct {
		name  string
		ratio float64
	}{
		{
			name:  "negative",
			ratio: -0.1,
		},
		{
			name:  "above one",
			ratio: 1.1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{samplingRatio: 1.0}

			err := WithSamplingRatio(tt.ratio)(p)
			if err == nil {
				t.Fatal("WithSamplingRatio() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "sampling ratio must be in [0, 1]") {
				t.Errorf("WithSamplingRatio() error = %q, want the validation message", err)
			}
			if p.samplingRatio != 1.0 {
				t.Errorf("samplingRatio = %g, want it unchanged", p.samplingRatio)
			}
		})
	}
}

func Test_WithShutdownTimeout_SetsValue(t *testing.T) {
	p := &provider{}

	if err := WithShutdownTimeout(3 * time.Second)(p); err != nil {
		t.Fatalf("WithShutdownTimeout() error = %v", err)
	}
	if p.shutdownTimeout != 3*time.Second {
		t.Errorf("shutdownTimeout = %s, want %s", p.shutdownTimeout, 3*time.Second)
	}
}

func Test_WithShutdownTimeout_RejectsNonPositive(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
	}{
		{
			name:    "zero",
			timeout: 0,
		},
		{
			name:    "negative",
			timeout: -time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &provider{shutdownTimeout: 10 * time.Second}

			err := WithShutdownTimeout(tt.timeout)(p)
			if err == nil {
				t.Fatal("WithShutdownTimeout() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), "shutdown timeout must be positive") {
				t.Errorf("WithShutdownTimeout() error = %q, want the validation message", err)
			}
			if p.shutdownTimeout != 10*time.Second {
				t.Errorf("shutdownTimeout = %s, want it unchanged", p.shutdownTimeout)
			}
		})
	}
}

func Test_WithLogger_SetsValue(t *testing.T) {
	lg := newBufferLogger(t, io.Discard)
	p := &provider{}

	if err := WithLogger(lg)(p); err != nil {
		t.Fatalf("WithLogger() error = %v", err)
	}
	if p.logger != lg {
		t.Error("logger does not match the configured one")
	}
}

func Test_WithLogger_RejectsNil(t *testing.T) {
	p := &provider{}

	err := WithLogger(nil)(p)
	if err == nil {
		t.Fatal("WithLogger() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "lifecycle logger is required") {
		t.Errorf("WithLogger() error = %q, want the validation message", err)
	}
	if p.logger != nil {
		t.Error("logger = non-nil, want it unset")
	}
}

func Test_New_InvalidOption(t *testing.T) {
	tests := []struct {
		name    string
		option  Option
		wantErr string
	}{
		{
			name:    "empty endpoint",
			option:  WithEndpoint(""),
			wantErr: "endpoint is required",
		},
		{
			name:    "endpoint without port",
			option:  WithEndpoint("localhost"),
			wantErr: "endpoint must be in host:port form",
		},
		{
			name:    "nil TLS config",
			option:  WithTLS(nil),
			wantErr: "TLS config is required",
		},
		{
			name:    "sampling ratio out of range",
			option:  WithSamplingRatio(1.5),
			wantErr: "sampling ratio must be in [0, 1]",
		},
		{
			name:    "non-positive shutdown timeout",
			option:  WithShutdownTimeout(0),
			wantErr: "shutdown timeout must be positive",
		},
		{
			name:    "nil lifecycle logger",
			option:  WithLogger(nil),
			wantErr: "lifecycle logger is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.option)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
