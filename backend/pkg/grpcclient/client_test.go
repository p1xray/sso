package grpcclient

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

func Test_New_ConnectsToRunningServer(t *testing.T) {
	address := startTestServer(t)

	cl, err := New(context.Background(),
		WithTarget(address),
		WithInsecure(),
		WithConnectAttempts(3),
		WithConnectAttemptTimeout(10*time.Millisecond),
		WithMaxBackoff(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cl.Close()

	// The value plugs into generated client constructors, which accept
	// grpc.ClientConnInterface, without Unwrap.
	acceptGeneratedClient(cl)

	var reply string
	err = cl.Invoke(context.Background(), "/grpcclient.test.EchoService/Echo",
		"ping", &reply, grpc.CallContentSubtype(stringCodec{}.Name()))
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if reply != "ping" {
		t.Errorf("reply = %q, want %q", reply, "ping")
	}

	if cl.Unwrap() == nil {
		t.Error("Unwrap() = nil, want the underlying connection")
	}
}

func Test_New_WaitsForDelayedServer(t *testing.T) {
	address := startDelayedTestServer(t, 300*time.Millisecond)
	logs := new(bytes.Buffer)

	cl, err := New(context.Background(),
		WithTarget(address),
		WithInsecure(),
		WithConnectAttempts(10),
		WithConnectAttemptTimeout(20*time.Millisecond),
		WithMaxBackoff(80*time.Millisecond),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err != nil {
		t.Fatalf("New() error = %v, want the retry loop to outlast the delay", err)
	}
	defer cl.Close()

	out := logs.String()
	for _, want := range []string{"target is unreachable", "connected"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want them to contain %q", out, want)
		}
	}
}

func Test_New_UnreachableTargetExhaustsAttempts(t *testing.T) {
	// Nothing listens on the port, so every attempt has to fail.
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	logs := new(bytes.Buffer)

	start := time.Now()
	_, err := New(context.Background(),
		WithTarget(address),
		WithInsecure(),
		WithConnectAttempts(3),
		WithConnectAttemptTimeout(10*time.Millisecond),
		WithMaxBackoff(30*time.Millisecond),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err == nil {
		t.Fatal("New() error = nil, want the attempts to run out")
	}
	if !strings.Contains(err.Error(), "grpcclient: connect after 3 attempts") {
		t.Errorf("New() error = %q, want the attempts error wrapped by the package", err)
	}
	if !strings.Contains(logs.String(), "target is unreachable") {
		t.Errorf("logs = %q, want them to contain the failed attempts", logs.String())
	}

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("New() took %s, want the knobs to bound it", elapsed)
	}
}

func Test_New_CanceledContextFailsFast(t *testing.T) {
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := New(ctx,
		WithTarget(address),
		WithInsecure(),
		WithMaxBackoff(50*time.Millisecond),
	)
	if err == nil {
		t.Fatal("New() error = nil, want the cancellation to fail it")
	}
	if !strings.Contains(err.Error(), "grpcclient: connect:") {
		t.Errorf("New() error = %q, want the cancellation error wrapped by the package", err)
	}
}

func Test_New_InvalidTargetDelegatedToGRPC(t *testing.T) {
	// A control character breaks the target parse, which grpc itself rejects.
	_, err := New(context.Background(),
		WithTarget("127.0.0.1:9090\n"),
		WithInsecure(),
	)
	if err == nil {
		t.Fatal("New() error = nil, want a parse failure")
	}
	if !strings.Contains(err.Error(), "grpcclient: create connection:") {
		t.Errorf("New() error = %q, want the parse error wrapped by the package", err)
	}
}

func Test_Client_CloseSemantics(t *testing.T) {
	cl, err := New(context.Background(),
		WithTarget(startTestServer(t)),
		WithInsecure(),
		WithConnectAttempts(3),
		WithConnectAttemptTimeout(10*time.Millisecond),
		WithMaxBackoff(500*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := cl.Close(); err != nil {
		t.Errorf("first Close() error = %v, want nil", err)
	}

	// grpc answers a second Close with its deprecated ErrClientConnClosing;
	// only the non-nil outcome is the contract here.
	if err := cl.Close(); err == nil {
		t.Error("second Close() error = nil, want non-nil")
	}
}

func Test_Client_LogsConnected(t *testing.T) {
	address := startTestServer(t)
	logs := new(bytes.Buffer)

	cl, err := New(context.Background(),
		WithTarget(address),
		WithInsecure(),
		WithConnectAttempts(3),
		WithConnectAttemptTimeout(10*time.Millisecond),
		WithMaxBackoff(500*time.Millisecond),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer cl.Close()

	out := logs.String()
	for _, want := range []string{"connected", address} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want them to contain %q", out, want)
		}
	}
}

// acceptGeneratedClient matches the parameter type of the generated client
// constructors, proving the client plugs into them directly.
func acceptGeneratedClient(grpc.ClientConnInterface) {}

// stringCodec carries plain strings as payloads, keeping the tests free of any
// protobuf dependency.
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

// echoServer is the service the tests serve: one unary echo method.
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
	ServiceName: "grpcclient.test.EchoService",
	HandlerType: (*echoServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    echoHandler,
		},
	},
}

// startTestServer starts an echo gRPC server on a free local port and returns
// its address. The server is stopped on test cleanup.
func startTestServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", freePort(t)))
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}

	srv := grpc.NewServer()
	srv.RegisterService(&echoServiceDesc, echoImpl{})
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(srv.Stop)

	return listener.Addr().String()
}

// startDelayedTestServer binds the port at once but starts serving only after
// delay, modeling a dependency that is slow to come up: early connect attempts
// fail and the retry loop has to carry New through.
func startDelayedTestServer(t *testing.T, delay time.Duration) string {
	t.Helper()

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", freePort(t)))
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}

	srv := grpc.NewServer()
	srv.RegisterService(&echoServiceDesc, echoImpl{})
	go func() {
		time.Sleep(delay)
		_ = srv.Serve(listener)
	}()
	t.Cleanup(srv.Stop)

	return listener.Addr().String()
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
