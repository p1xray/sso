package tracing

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/p1xray/sso/backend/pkg/logger"

	"go.opentelemetry.io/otel/trace"
	tracecollectv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
)

// fakeCollector is a hand-made OTLP trace collector: it records every export
// request and can be told to fail or to hang, which is all the tests need from
// a real Tempo.
type fakeCollector struct {
	tracecollectv1.UnimplementedTraceServiceServer

	// err, when set, is returned from every Export call.
	err error

	// delay, when positive, holds every Export call before it answers.
	delay time.Duration

	mu            sync.Mutex
	resourceSpans []*tracev1.ResourceSpans
}

// Export records the resource spans of the request, honoring the configured
// error and delay.
func (f *fakeCollector) Export(ctx context.Context, req *tracecollectv1.ExportTraceServiceRequest) (*tracecollectv1.ExportTraceServiceResponse, error) {
	if f.delay > 0 {
		timer := time.NewTimer(f.delay)
		defer timer.Stop()

		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}

	if f.err != nil {
		return nil, f.err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.resourceSpans = append(f.resourceSpans, req.ResourceSpans...)

	return &tracecollectv1.ExportTraceServiceResponse{}, nil
}

// resourceAttributes flattens the resource attributes of every recorded
// resource span into a key-value map.
func (f *fakeCollector) resourceAttributes() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	attrs := make(map[string]string)
	for _, rs := range f.resourceSpans {
		if rs.Resource == nil {
			continue
		}
		for _, kv := range rs.Resource.Attributes {
			attrs[kv.GetKey()] = kv.Value.GetStringValue()
		}
	}

	return attrs
}

// spanTraceIDs counts the recorded spans per trace ID.
func (f *fakeCollector) spanTraceIDs() map[trace.TraceID]int {
	f.mu.Lock()
	defer f.mu.Unlock()

	counts := make(map[trace.TraceID]int)
	for _, rs := range f.resourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				counts[trace.TraceID(span.TraceId)]++
			}
		}
	}

	return counts
}

// startFakeCollector serves fc over gRPC on a free local port and returns its
// address. The server is stopped on test cleanup.
func startFakeCollector(t *testing.T, fc *fakeCollector) string {
	t.Helper()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", freePort(t)))
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}

	srv := grpc.NewServer()
	tracecollectv1.RegisterTraceServiceServer(srv, fc)
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	return listener.Addr().String()
}

// remoteSampledContext models an upstream decision delivered by a traceparent
// header: a remote span context with the sampled flag set. The fixed IDs come
// from the W3C Trace Context spec examples, keeping the assertions
// deterministic.
func remoteSampledContext(t *testing.T) (context.Context, trace.TraceID) {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("trace ID parse error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("span ID parse error = %v", err)
	}

	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})

	return trace.ContextWithSpanContext(context.Background(), spanContext), traceID
}

// newBufferLogger returns a logger writing JSON records to buf.
func newBufferLogger(t *testing.T, buf io.Writer) logger.Logger {
	t.Helper()

	lg, err := logger.New(logger.Config{
		Service: "tracing-test",
		Env:     logger.EnvDev,
		Format:  logger.FormatJSON,
		Writer:  buf,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	return lg
}

// freePort returns a localhost port that is currently free: the listener is
// closed before the port is used, so the test binds it without a conflict.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener error = %v", err)
	}

	return port
}
