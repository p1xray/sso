package tracing

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
)

func Test_New_RejectsMissingRequiredOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{
			name: "no endpoint",
			opts: []Option{
				WithInsecure(),
				WithServiceName("auth"),
				WithServiceVersion("v0.1.0"),
				WithEnvironment("dev"),
			},
			wantErr: "tracing: endpoint is required",
		},
		{
			name: "no transport security choice",
			opts: []Option{
				WithEndpoint("127.0.0.1:4317"),
				WithServiceName("auth"),
				WithServiceVersion("v0.1.0"),
				WithEnvironment("dev"),
			},
			wantErr: "tracing: transport security choice is required",
		},
		{
			name: "no service name",
			opts: []Option{
				WithEndpoint("127.0.0.1:4317"),
				WithInsecure(),
				WithServiceVersion("v0.1.0"),
				WithEnvironment("dev"),
			},
			wantErr: "tracing: service name is required",
		},
		{
			name: "no service version",
			opts: []Option{
				WithEndpoint("127.0.0.1:4317"),
				WithInsecure(),
				WithServiceName("auth"),
				WithEnvironment("dev"),
			},
			wantErr: "tracing: service version is required",
		},
		{
			name: "no environment",
			opts: []Option{
				WithEndpoint("127.0.0.1:4317"),
				WithInsecure(),
				WithServiceName("auth"),
				WithServiceVersion("v0.1.0"),
			},
			wantErr: "tracing: environment is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), tt.opts...)
			if err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("New() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func Test_New_SucceedsWhenEndpointUnreachable(t *testing.T) {
	// Nothing listens on the port, modeling an LGTM stack that is down: the
	// provider must still start at once.
	endpoint := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	start := time.Now()
	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v, want the provider to start without the collector", err)
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("New() took %s, want it not to wait for the collector", elapsed)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() error = %v, want nil", err)
	}
}

func Test_New_ResourceCarriesServiceIdentity(t *testing.T) {
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v1.2.3"),
		WithEnvironment("staging"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, span := p.Tracer("tracing.test").Start(context.Background(), "work")
	span.End()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	attrs := fc.resourceAttributes()
	tests := []struct {
		key  string
		want string
	}{
		{key: "service.name", want: "auth"},
		{key: "service.version", want: "v1.2.3"},
		{key: "deployment.environment", want: "staging"},
		// The merge must keep the default resource attributes, not replace them.
		{key: "telemetry.sdk.name", want: "opentelemetry"},
	}
	for _, tt := range tests {
		if got := attrs[tt.key]; got != tt.want {
			t.Errorf("resource attribute %s = %q, want %q", tt.key, got, tt.want)
		}
	}

	// The merge must also overwrite the default service name, not duplicate it.
	if got := attrs["service.name"]; got != "auth" {
		t.Errorf("resource attribute service.name = %q, want the explicit value to win over unknown_service", got)
	}
}

func Test_New_RootSpansFollowRatio(t *testing.T) {
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithSamplingRatio(0),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// A span with no parent is a root: the ratio sampler decides alone.
	_, span := p.Tracer("tracing.test").Start(context.Background(), "work")
	span.End()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if counts := fc.spanTraceIDs(); len(counts) != 0 {
		t.Errorf("exported spans = %d, want 0: a zero ratio must drop root spans", len(counts))
	}
}

func Test_New_SampledParentIsRecordedWhenRatioZero(t *testing.T) {
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithSamplingRatio(0),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	parentCtx, parentTraceID := remoteSampledContext(t)
	_, span := p.Tracer("tracing.test").Start(parentCtx, "work")
	span.End()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	// The ratio is zero, so only the parent's sampled flag can keep this span
	// alive: a trace sampled upstream must not be broken downstream.
	if got := fc.spanTraceIDs()[parentTraceID]; got != 1 {
		t.Errorf("spans with the parent trace ID = %d, want 1", got)
	}
}

func Test_New_PropagatesTraceContextAcrossGRPC(t *testing.T) {
	fc := &fakeCollector{}
	collectorEndpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(collectorEndpoint),
		WithInsecure(),
		WithServiceName("gateway"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithSamplingRatio(0),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// The downstream service, instrumented like grpcserver consumers would be.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", freePort(t)))
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}
	srv := grpc.NewServer(grpc.StatsHandler(p.ServerStatsHandler()))
	srv.RegisterService(&echoServiceDesc, echoImpl{})
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	// The caller, instrumented like grpcclient consumers would be. The zero
	// ratio leaves only the remote sampled parent to keep the trace alive, so
	// the spans prove the decision traveled the hop.
	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(p.ClientStatsHandler()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}

	parentCtx, parentTraceID := remoteSampledContext(t)
	var reply string
	err = conn.Invoke(parentCtx, "/tracing.test.EchoService/Echo",
		"ping", &reply, grpc.CallContentSubtype(stringCodec{}.Name()))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if reply != "ping" {
		t.Errorf("reply = %q, want %q", reply, "ping")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("conn.Close() error = %v", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if got := fc.spanTraceIDs()[parentTraceID]; got < 2 {
		t.Errorf("spans with the parent trace ID = %d, want at least the client and the server span", got)
	}
}

func Test_Provider_HTTPHandler_ContinuesSampledTrace(t *testing.T) {
	// The collector is real, so the handler span exports and Shutdown is
	// instant instead of waiting out the export backoff of a dead endpoint.
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("gateway"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithSamplingRatio(0),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	parentCtx, parentTraceID := remoteSampledContext(t)

	var gotTraceID trace.TraceID
	var gotSampled bool
	handler := p.HTTPHandler("test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanContext := trace.SpanFromContext(r.Context()).SpanContext()
		gotTraceID = spanContext.TraceID()
		gotSampled = spanContext.IsSampled()

		w.WriteHeader(http.StatusOK)
	}))

	// The request carries the parent the way an upstream W3C client would.
	req := httptest.NewRequest(http.MethodGet, "http://test.local/", nil)
	p.propagator.Inject(parentCtx, propagation.HeaderCarrier(req.Header))

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if gotTraceID != parentTraceID {
		t.Errorf("handler trace ID = %s, want the parent %s", gotTraceID, parentTraceID)
	}
	if !gotSampled {
		t.Error("handler span is not sampled, want the parent's sampled flag to continue")
	}
}

func Test_Provider_TracerDelegates(t *testing.T) {
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tracer := p.Tracer("tracing.test")
	if tracer == nil {
		t.Fatal("Tracer() = nil, want a tracer")
	}
	_, span := tracer.Start(context.Background(), "work")
	span.End()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if counts := fc.spanTraceIDs(); len(counts) != 1 {
		t.Errorf("exported spans = %d, want 1", len(counts))
	}
}

func Test_Provider_StatsHandlersNotNil(t *testing.T) {
	p, err := New(context.Background(),
		WithEndpoint("127.0.0.1:4317"),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if p.ServerStatsHandler() == nil {
		t.Error("ServerStatsHandler() = nil, want the wired handler")
	}
	if p.ClientStatsHandler() == nil {
		t.Error("ClientStatsHandler() = nil, want the wired handler")
	}
}

func Test_Provider_ShutdownIsIdempotent(t *testing.T) {
	fc := &fakeCollector{}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("first Shutdown() error = %v, want nil", err)
	}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Errorf("second Shutdown() error = %v, want nil", err)
	}
}

func Test_Provider_Shutdown_BoundedByTimeout(t *testing.T) {
	// The collector holds every export, so only the timeout can end the flush.
	fc := &fakeCollector{delay: 5 * time.Second}
	endpoint := startFakeCollector(t, fc)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithShutdownTimeout(150*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, span := p.Tracer("tracing.test").Start(context.Background(), "work")
	span.End()

	start := time.Now()
	err = p.Shutdown(context.Background())
	if err == nil {
		t.Fatal("Shutdown() error = nil, want the timeout to fail the flush")
	}
	if !strings.Contains(err.Error(), "tracing: shutdown:") {
		t.Errorf("Shutdown() error = %q, want the package prefix", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Shutdown() took %s, want the timeout to bound it", elapsed)
	}
}

func Test_Provider_ExportFailureIsLogged(t *testing.T) {
	// InvalidArgument is not retryable, so the failure is reported at once
	// instead of after the export backoff.
	fc := &fakeCollector{err: status.Error(codes.InvalidArgument, "invalid span")}
	endpoint := startFakeCollector(t, fc)
	logs := new(bytes.Buffer)

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, span := p.Tracer("tracing.test").Start(context.Background(), "work")
	span.End()

	_ = p.Shutdown(context.Background())

	out := logs.String()
	for _, want := range []string{"span export failed", "invalid span"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want them to contain %q", out, want)
		}
	}
}

func Test_Provider_UnwrapReturnsUnderlying(t *testing.T) {
	p, err := New(context.Background(),
		WithEndpoint("127.0.0.1:4317"),
		WithInsecure(),
		WithServiceName("auth"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = p.Shutdown(context.Background()) })

	if p.Unwrap() == nil {
		t.Error("Unwrap() = nil, want the underlying tracer provider")
	}
}

// Test_Integration_OTLPEndpoint runs the full cycle against a real collector,
// e.g. a local LGTM stack:
//
//	docker run --rm -p 4317:4317 grafana/otel-lgtm
//	TEST_OTLP_ENDPOINT=localhost:4317 go test -run Integration -v ./...
func Test_Integration_OTLPEndpoint(t *testing.T) {
	endpoint := os.Getenv("TEST_OTLP_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_OTLP_ENDPOINT is not set")
	}

	p, err := New(context.Background(),
		WithEndpoint(endpoint),
		WithInsecure(),
		WithServiceName("tracing-integration"),
		WithServiceVersion("v0.1.0"),
		WithEnvironment("dev"),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, span := p.Tracer("tracing.test").Start(context.Background(), "integration-check")
	span.End()

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

// stringCodec carries plain strings as payloads, keeping the echo service free
// of any protobuf dependency.
type stringCodec struct{}

// Name is registered in init, so both dial peers pick the codec through the
// content subtype.
func (stringCodec) Name() string { return "string" }

func (stringCodec) Marshal(v any) ([]byte, error) { return []byte(v.(string)), nil }

func (stringCodec) Unmarshal(data []byte, v any) error {
	*(v.(*string)) = string(data)
	return nil
}

func init() {
	encoding.RegisterCodec(stringCodec{})
}

// echoServer is the service the propagation test serves: one unary echo
// method.
type echoServer interface {
	Echo(context.Context, string) (string, error)
}

// echoImpl echoes the request back.
type echoImpl struct{}

// Echo returns in unchanged.
func (echoImpl) Echo(_ context.Context, in string) (string, error) {
	return in, nil
}

// echoHandler adapts the string payload to grpc's generic method handler.
func echoHandler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	in := new(string)
	if err := dec(in); err != nil {
		return nil, err
	}

	return srv.(echoServer).Echo(ctx, *in)
}

// echoServiceDesc is a minimal unary service: no proto, no health package.
var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: "tracing.test.EchoService",
	HandlerType: (*echoServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    echoHandler,
		},
	},
}
