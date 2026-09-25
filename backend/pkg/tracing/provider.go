// Package tracing provides unified OpenTelemetry trace collection on top of
// go.opentelemetry.io/otel, exporting spans over OTLP gRPC.
//
// New builds a tracer provider from explicitly set options: the endpoint, an
// explicit transport security choice and the service identity (name, version,
// environment) are required, and the identity becomes the resource of every
// exported span. Sampling is parent-based: the ratio decides only for root
// spans, spans with a parent follow the parent's sampled flag, and the W3C
// TraceContext propagator carries the decision across gRPC and HTTP hops, so a
// trace sampled at the edge is never broken downstream.
//
// Startup is best-effort: the exporter connects to the collector in the
// background and retries failed exports on its own, so an unreachable endpoint
// neither fails New nor blocks it. Export failures are reported through the
// logger set with WithLogger.
//
// Provider delegates the tracer entry point and hands out the transport
// instrumentation helpers for grpcserver, grpcclient and httpserver. Unwrap
// returns the underlying *sdktrace.TracerProvider for everything else.
package tracing

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"
	"github.com/p1xray/sso/backend/pkg/logger/sl"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/stats"
)

const (
	// defaultSamplingRatio is the fraction of root traces sampled when no ratio
	// is configured. Everything is sampled, matching the all-write dev LGTM
	// stack; production tunes it down.
	defaultSamplingRatio = 1.0

	// defaultShutdownTimeout is how long Shutdown waits for the buffered spans
	// to reach the collector before giving up.
	defaultShutdownTimeout = 10 * time.Second
)

// Provider provides access to the OpenTelemetry tracer provider and the
// transport instrumentation helpers wired to it.
type Provider interface {
	// Tracer returns a tracer that records spans with this provider. The name
	// should identify the instrumentation scope, e.g. the component that owns
	// the spans.
	Tracer(name string, opts ...trace.TracerOption) trace.Tracer

	// ServerStatsHandler returns the gRPC stats handler that extracts the
	// remote parent from the incoming trace context and starts a server span
	// for every RPC. Plug it in with
	// grpcserver.WithGRPCOptions(grpc.StatsHandler(...)).
	ServerStatsHandler() stats.Handler

	// ClientStatsHandler returns the gRPC stats handler that starts a client
	// span for every outgoing RPC and injects it into the trace context, which
	// is what carries the sampling decision to the downstream services. Plug
	// it in with grpcclient.WithGRPCOptions(grpc.WithStatsHandler(...)).
	ClientStatsHandler() stats.Handler

	// HTTPHandler wraps handler so every request gets a server span named
	// after operation. Plug it in with httpserver.WithHandler(...).
	HTTPHandler(operation string, handler http.Handler) http.Handler

	// Shutdown flushes the buffered spans to the collector and releases the
	// provider resources. The whole flush is bounded by the shutdown timeout
	// (see WithShutdownTimeout); it is safe to call Shutdown more than once.
	Shutdown(ctx context.Context) error

	// Unwrap returns the underlying *sdktrace.TracerProvider.
	Unwrap() *sdktrace.TracerProvider
}

type provider struct {
	tracerProvider     *sdktrace.TracerProvider
	serverStatsHandler stats.Handler
	clientStatsHandler stats.Handler
	propagator         propagation.TextMapPropagator

	endpoint       string
	insecure       bool
	tlsConfig      *tls.Config
	headers        map[string]string
	serviceName    string
	serviceVersion string
	environment    string
	samplingRatio  float64

	shutdownTimeout time.Duration
	logger          logger.Logger
}

// compile-time check that the concrete type satisfies the contract.
var _ Provider = (*provider)(nil)

// New builds a tracer provider from opts and wires the OTLP gRPC exporter. The
// endpoint, an explicit transport security choice and the service identity are
// required. The collector is not contacted here: the exporter connects in the
// background and retries failed exports, so an unreachable endpoint neither
// fails nor blocks New.
func New(ctx context.Context, opts ...Option) (*provider, error) {
	p := &provider{
		samplingRatio:   defaultSamplingRatio,
		shutdownTimeout: defaultShutdownTimeout,
	}

	for _, opt := range opts {
		if err := opt(p); err != nil {
			return nil, fmt.Errorf("tracing: %w", err)
		}
	}

	if p.endpoint == "" {
		return nil, fmt.Errorf("tracing: endpoint is required")
	}
	if !p.insecure && p.tlsConfig == nil {
		return nil, fmt.Errorf("tracing: transport security choice is required (use WithInsecure or WithTLS)")
	}
	if p.serviceName == "" {
		return nil, fmt.Errorf("tracing: service name is required")
	}
	if p.serviceVersion == "" {
		return nil, fmt.Errorf("tracing: service version is required")
	}
	if p.environment == "" {
		return nil, fmt.Errorf("tracing: environment is required")
	}

	// The service attributes are schemaless, so the merge keeps the schema URL
	// of the default resource instead of conflicting with it, while the
	// explicit values overwrite the defaults: Merge lets b win over a.
	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(p.serviceName),
			semconv.ServiceVersion(p.serviceVersion),
			semconv.DeploymentEnvironment(p.environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("tracing: merge resource: %w", err)
	}

	exporter, err := otlptracegrpc.New(ctx, p.exporterOptions()...)
	if err != nil {
		return nil, fmt.Errorf("tracing: create exporter: %w", err)
	}

	p.tracerProvider = sdktrace.NewTracerProvider(
		// The ratio decides only for root spans; spans with a parent follow
		// the parent's sampled flag, so a trace sampled at the gateway is
		// never broken in the downstream services.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(p.samplingRatio))),
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	)

	// The propagator is passed to the handlers explicitly instead of relying
	// on the otel globals: the package owns the wiring, and tests stay
	// isolated from process-wide state.
	p.propagator = propagation.TraceContext{}
	p.serverStatsHandler = otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(p.tracerProvider),
		otelgrpc.WithPropagators(p.propagator),
	)
	p.clientStatsHandler = otelgrpc.NewClientHandler(
		otelgrpc.WithTracerProvider(p.tracerProvider),
		otelgrpc.WithPropagators(p.propagator),
	)

	if p.logger != nil {
		// The batch span processor reports export failures only through the
		// otel global error handler, so route it into the service logger.
		otel.SetErrorHandler(otel.ErrorHandlerFunc(p.handleExportError))
	}

	p.logStarted(ctx)

	return p, nil
}

// Tracer returns a tracer that records spans with this provider.
func (p *provider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return p.tracerProvider.Tracer(name, opts...)
}

// ServerStatsHandler returns the wired gRPC server stats handler.
func (p *provider) ServerStatsHandler() stats.Handler {
	return p.serverStatsHandler
}

// ClientStatsHandler returns the wired gRPC client stats handler.
func (p *provider) ClientStatsHandler() stats.Handler {
	return p.clientStatsHandler
}

// HTTPHandler wraps handler so every request gets a server span named after
// operation.
func (p *provider) HTTPHandler(operation string, handler http.Handler) http.Handler {
	return otelhttp.NewHandler(handler, operation,
		otelhttp.WithTracerProvider(p.tracerProvider),
		otelhttp.WithPropagators(p.propagator),
	)
}

// Shutdown flushes the buffered spans to the collector and releases the
// provider resources. The whole flush is bounded by the shutdown timeout.
func (p *provider) Shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, p.shutdownTimeout)
	defer cancel()

	if err := p.tracerProvider.Shutdown(shutdownCtx); err != nil {
		p.logError(ctx, "tracing: shutdown failed", err)
		return fmt.Errorf("tracing: shutdown: %w", err)
	}

	return nil
}

// Unwrap returns the underlying *sdktrace.TracerProvider.
func (p *provider) Unwrap() *sdktrace.TracerProvider {
	return p.tracerProvider
}

// exporterOptions turns the transport choices into exporter options: the
// endpoint, the credentials and the metadata of every export request.
func (p *provider) exporterOptions() []otlptracegrpc.Option {
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(p.endpoint),
	}

	if p.insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	} else {
		opts = append(opts, otlptracegrpc.WithTLSCredentials(credentials.NewTLS(p.tlsConfig)))
	}

	if p.headers != nil {
		opts = append(opts, otlptracegrpc.WithHeaders(p.headers))
	}

	return opts
}

// handleExportError reports a failed span export through the configured
// logger. The otel error handler has no request context, so the background one
// is the best available.
func (p *provider) handleExportError(err error) {
	if p.logger == nil {
		return
	}

	p.logger.Warn(context.Background(), "tracing: span export failed", sl.Err(err))
}

// logStarted announces the provider through the configured logger, if any.
func (p *provider) logStarted(ctx context.Context) {
	if p.logger == nil {
		return
	}

	p.logger.Info(ctx, "tracing: provider started",
		slog.String("endpoint", p.endpoint),
		slog.String("service", p.serviceName),
		slog.Float64("sampling_ratio", p.samplingRatio),
	)
}

// logError reports err through the configured logger, if any.
func (p *provider) logError(ctx context.Context, msg string, err error) {
	if p.logger == nil {
		return
	}

	p.logger.Error(ctx, msg, sl.Err(err))
}
