package httpserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func Test_Server_StartsAndStopsGracefully(t *testing.T) {
	// No logger is configured: the lifecycle must stay silent.
	srv, err := New(
		WithAddress(fmt.Sprintf("127.0.0.1:%d", freePort(t))),
		WithHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case err := <-srv.Notify():
		t.Fatalf("Notify() delivered %v while the server was running", err)
	case <-time.After(100 * time.Millisecond):
	}

	srv.Stop()

	select {
	case err := <-srv.Notify():
		if err != nil {
			t.Errorf("Notify() = %v, want nil for a graceful stop", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Notify() did not deliver a value after Stop()")
	}

	// The channel is closed after the outcome is delivered.
	if err, ok := <-srv.Notify(); ok {
		t.Errorf("Notify() after the outcome = (%v, true), want a closed channel", err)
	}
}

func Test_Server_ServesRequestsUsingHandler(t *testing.T) {
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	srv, err := New(
		WithAddress(address),
		WithHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "pong")
		})),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop()

	if body := getWhenReady(t, "http://"+address); body != "pong" {
		t.Errorf("response body = %q, want %q", body, "pong")
	}
}

func Test_Server_DoubleStartReturnsError(t *testing.T) {
	srv, err := New(
		WithAddress(fmt.Sprintf("127.0.0.1:%d", freePort(t))),
		WithHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer srv.Stop()

	if err := srv.Start(context.Background()); err == nil {
		t.Error("second Start() error = nil, want an error")
	}
}

func Test_Server_ListenFailureNotifiesError(t *testing.T) {
	// Holding the listener open makes the port busy for the server under test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}
	defer ln.Close()

	srv, err := New(
		WithAddress(ln.Addr().String()),
		WithHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case err := <-srv.Notify():
		if err == nil {
			t.Fatal("Notify() = nil, want a listen failure")
		}
		if !strings.Contains(err.Error(), "httpserver: failed to listen") {
			t.Errorf("Notify() error = %q, want it wrapped by the package", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Notify() did not deliver a listen failure")
	}

	// Stopping a server that never started serving must not hang.
	srv.Stop()
}

func Test_Server_StopImmediatelyAfterStartNotifiesNil(t *testing.T) {
	srv, err := New(
		WithAddress(fmt.Sprintf("127.0.0.1:%d", freePort(t))),
		WithHandler(http.NotFoundHandler()),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Stop can win the race against the serve goroutine; the outcome must
	// still be a graceful stop, not http.ErrServerClosed.
	srv.Stop()

	select {
	case err := <-srv.Notify():
		if err != nil {
			t.Errorf("Notify() = %v, want nil when Stop raced the serve goroutine", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Notify() did not deliver a value after Stop()")
	}
}

func Test_Server_StopForcesShutdownAfterTimeout(t *testing.T) {
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	release := make(chan struct{})
	defer close(release)

	srv, err := New(
		WithAddress(address),
		WithHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The busy handler keeps the graceful drain from completing, so
			// the shutdown timeout has to force it.
			<-release
		})),
		WithShutdownTimeout(50*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	conn := dialWhenReady(t, address)
	defer conn.Close()

	if _, err := conn.Write([]byte("GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n")); err != nil {
		t.Fatalf("write request error = %v", err)
	}
	// Give the server time to dispatch the request to the handler.
	time.Sleep(100 * time.Millisecond)

	start := time.Now()
	srv.Stop()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("Stop() took %s, want the 50ms shutdown timeout to bound it", elapsed)
	}

	select {
	case err := <-srv.Notify():
		if err != nil {
			t.Errorf("Notify() = %v, want nil after the forced shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Notify() did not deliver a value after the forced shutdown")
	}
}

func Test_Server_LogsLifecycleStart(t *testing.T) {
	logs := new(bytes.Buffer)
	address := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	srv, err := New(
		WithAddress(address),
		WithHandler(http.NotFoundHandler()),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	srv.Stop()
	<-srv.Notify()

	out := logs.String()
	for _, want := range []string{"server started", address} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want them to contain %q", out, want)
		}
	}
}

func Test_Server_LogsListenFailure(t *testing.T) {
	// Holding the listener open makes the port busy for the server under test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen() error = %v", err)
	}
	defer ln.Close()

	logs := new(bytes.Buffer)
	srv, err := New(
		WithAddress(ln.Addr().String()),
		WithHandler(http.NotFoundHandler()),
		WithLogger(newBufferLogger(t, logs)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	<-srv.Notify()

	out := logs.String()
	for _, want := range []string{"server start failed", "failed to listen"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs = %q, want them to contain %q", out, want)
		}
	}
}

// freePort returns a localhost port that is currently free: the listener is
// closed before the port is used, so the server binds it without a conflict.
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

// dialWhenReady connects to addr, retrying until the server is listening.
func dialWhenReady(t *testing.T, addr string) net.Conn {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial() error = %v, want the server to be listening", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// getWhenReady performs a GET request to url, retrying until the server is
// listening, and returns the response body.
func getWhenReady(t *testing.T, url string) string {
	t.Helper()

	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Get(url)
		if err == nil {
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatalf("read body error = %v", err)
			}
			return string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("get() error = %v, want the server to be listening", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
