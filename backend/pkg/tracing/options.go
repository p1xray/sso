package tracing

import (
	"crypto/tls"
	"fmt"
	"maps"
	"net"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
)

// Option customizes the provider construction in New.
type Option func(*provider) error

// WithEndpoint sets the address of the OTLP gRPC collector in host:port form,
// e.g. "localhost:4317". The endpoint is required.
func WithEndpoint(endpoint string) Option {
	return func(p *provider) error {
		if endpoint == "" {
			return fmt.Errorf("endpoint is required")
		}
		if _, _, err := net.SplitHostPort(endpoint); err != nil {
			return fmt.Errorf("endpoint must be in host:port form, got %q", endpoint)
		}

		p.endpoint = endpoint
		return nil
	}
}

// WithInsecure disables transport security for the OTLP connection: no
// encryption and no collector authentication. For local development and trusted
// internal networks. Mutually exclusive with WithTLS.
func WithInsecure() Option {
	return func(p *provider) error {
		if p.tlsConfig != nil {
			return fmt.Errorf("transport security is already configured")
		}

		p.insecure = true
		return nil
	}
}

// WithTLS sets TLS as the transport security for the OTLP connection with the
// given config. Mutually exclusive with WithInsecure.
func WithTLS(config *tls.Config) Option {
	return func(p *provider) error {
		if config == nil {
			return fmt.Errorf("TLS config is required")
		}
		if p.insecure {
			return fmt.Errorf("transport security is already configured")
		}

		p.tlsConfig = config
		return nil
	}
}

// WithHeaders sets the gRPC metadata sent with every export request, e.g.
// X-Scope-OrgID for a multi-tenant Tempo. The map is copied, so later changes
// to it do not affect the provider.
func WithHeaders(headers map[string]string) Option {
	return func(p *provider) error {
		if len(headers) == 0 {
			return fmt.Errorf("at least one header is required")
		}

		p.headers = maps.Clone(headers)
		return nil
	}
}

// WithServiceName sets the service.name resource attribute - the logical name
// of the service the spans belong to. Required.
func WithServiceName(name string) Option {
	return func(p *provider) error {
		if name == "" {
			return fmt.Errorf("service name is required")
		}

		p.serviceName = name
		return nil
	}
}

// WithServiceVersion sets the service.version resource attribute - the version
// string of the service the spans belong to. Required.
func WithServiceVersion(version string) Option {
	return func(p *provider) error {
		if version == "" {
			return fmt.Errorf("service version is required")
		}

		p.serviceVersion = version
		return nil
	}
}

// WithEnvironment sets the deployment.environment resource attribute - the
// deployment tier the service runs in (dev, staging, production). Required.
func WithEnvironment(environment string) Option {
	return func(p *provider) error {
		if environment == "" {
			return fmt.Errorf("environment is required")
		}

		p.environment = environment
		return nil
	}
}

// WithSamplingRatio sets the fraction of root traces sampled. Spans with a
// parent always follow the parent's sampled flag regardless of the ratio.
// Defaults to 1.0 (everything).
func WithSamplingRatio(ratio float64) Option {
	return func(p *provider) error {
		if ratio < 0 || ratio > 1 {
			return fmt.Errorf("sampling ratio must be in [0, 1], got %g", ratio)
		}

		p.samplingRatio = ratio
		return nil
	}
}

// WithShutdownTimeout sets how long Shutdown waits for the buffered spans to
// reach the collector before giving up. Defaults to 10s.
func WithShutdownTimeout(timeout time.Duration) Option {
	return func(p *provider) error {
		if timeout <= 0 {
			return fmt.Errorf("shutdown timeout must be positive, got %s", timeout)
		}

		p.shutdownTimeout = timeout
		return nil
	}
}

// WithLogger sets the logger for provider lifecycle events: startup and export
// failures. Nil (the default) keeps the lifecycle silent.
func WithLogger(lg logger.Logger) Option {
	return func(p *provider) error {
		if lg == nil {
			return fmt.Errorf("lifecycle logger is required")
		}

		p.logger = lg
		return nil
	}
}
