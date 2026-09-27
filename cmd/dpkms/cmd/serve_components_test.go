package cmd

import (
	"bytes"
	"context"
	"errors"
	"net"
	gohttp "net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	wsserver "github.com/ideacrafterslabs/ctxt/internal/server/ws"
)

// syncBuffer is a goroutine-safe log sink for the runner.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startHTTP returns an HTTP component on a fresh loopback listener and
// the listener's address.
func startHTTP(t *testing.T, drain time.Duration) (component, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	srv := &gohttp.Server{
		Handler: gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			w.WriteHeader(gohttp.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	t.Cleanup(func() { _ = srv.Close() })
	return httpComponent("http", srv, ln, drain), ln.Addr().String()
}

func httpAnswers(addr string) bool {
	c := gohttp.Client{Timeout: 500 * time.Millisecond, Transport: &gohttp.Transport{DisableKeepAlives: true}}
	resp, err := c.Get("http://" + addr + "/")
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return true
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// blockUntilCancelled is a well-behaved component: it runs until told to stop.
func blockUntilCancelled(name string) component {
	return component{name: name, run: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
}

// stuck ignores cancellation until the test ends, like a worker wedged on
// a job that never honours its context.
func stuck(t *testing.T, name string) component {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	return component{name: name, run: func(context.Context) error {
		<-release
		return nil
	}}
}

// runAsync runs r and returns a channel carrying its result.
func runAsync(r *componentRunner) <-chan error {
	done := make(chan error, 1)
	go func() { done <- r.run(context.Background()) }()
	return done
}

func awaitRun(t *testing.T, done <-chan error, within time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(within):
		t.Fatalf("serve did not exit within %s", within)
		return nil
	}
}

// A component that fails at start stops every component, HTTP included,
// logs the failure, and makes serve return it. Before the fix the HTTP
// server only stopped on a signal: serve kept answering on its port
// while every other component was gone, and never printed the failure.
func TestComponentRunner_StartFailureStopsHTTPAndReturnsError(t *testing.T) {
	httpComp, addr := startHTTP(t, 5*time.Second)

	// The cookie bridge's listener is lost before it can serve: its Serve
	// fails immediately, as a lost bind race did.
	bridgeLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = bridgeLn.Close()
	bridge := wsserver.NewCookieBridgeServer(wsserver.NewCookieCache(), nil)
	httpUp := make(chan struct{})

	log := &syncBuffer{}
	r := &componentRunner{
		components: []component{
			httpComp,
			blockUntilCancelled("workers"),
			{name: "cookie-bridge", run: func(ctx context.Context) error {
				<-httpUp // fail while HTTP is demonstrably serving
				return bridge.Serve(ctx, bridgeLn)
			}},
		},
		signals: make(chan os.Signal),
		drain:   5 * time.Second,
		log:     log,
	}
	done := runAsync(r)
	waitFor(t, "HTTP to serve", func() bool { return httpAnswers(addr) })
	close(httpUp)

	err = awaitRun(t, done, 3*time.Second)
	if err == nil {
		t.Fatal("serve returned nil after a component failed; want the failure")
	}
	if !strings.Contains(err.Error(), "cookie-bridge") {
		t.Errorf("error should name the failed component, got %v", err)
	}
	if !strings.Contains(log.String(), "error: cookie-bridge:") {
		t.Errorf("failure not logged; log = %q", log.String())
	}
	if httpAnswers(addr) {
		t.Error("HTTP still serving after a component failure")
	}
}

// A signal delivered after a component failure must still end serve,
// even while a component refuses to stop. Before the fix the signal
// goroutine had already returned, its channel stayed registered, and
// SIGTERM/SIGINT were swallowed; only SIGKILL ended the process.
func TestComponentRunner_SignalAfterFailureExits(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	failed := make(chan struct{})
	log := &syncBuffer{}
	r := &componentRunner{
		components: []component{
			stuck(t, "workers"),
			{name: "cookie-bridge", run: func(context.Context) error {
				defer close(failed)
				return errors.New("listen 127.0.0.1:1: bind: address already in use")
			}},
		},
		signals: sigs,
		drain:   time.Minute,
		log:     log,
	}
	done := runAsync(r)
	<-failed
	waitFor(t, "failure to be logged", func() bool { return strings.Contains(log.String(), "error: cookie-bridge:") })

	sigs <- syscall.SIGTERM
	err := awaitRun(t, done, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("exit error should carry the component failure, got %v", err)
	}
}

// Shutdown is bounded by the drain timeout even when a component never
// stops: serve exits non-zero instead of hanging.
func TestComponentRunner_DrainTimeoutBoundsShutdown(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	r := &componentRunner{
		components: []component{stuck(t, "workers"), blockUntilCancelled("grpc")},
		signals:    sigs,
		drain:      200 * time.Millisecond,
		log:        &syncBuffer{},
	}
	done := runAsync(r)
	sigs <- syscall.SIGTERM

	err := awaitRun(t, done, 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "timed out") || !strings.Contains(err.Error(), "workers") {
		t.Errorf("want a drain-timeout error naming the stuck component, got %v", err)
	}
}

// A signal with every component healthy is a clean shutdown: HTTP closes,
// serve returns nil, and context errors from stopping components are not
// reported as failures.
func TestComponentRunner_SignalStopsCleanly(t *testing.T) {
	httpComp, addr := startHTTP(t, 5*time.Second)
	sigs := make(chan os.Signal, 1)
	log := &syncBuffer{}
	r := &componentRunner{
		components: []component{httpComp, blockUntilCancelled("workers"), blockUntilCancelled("grpc")},
		signals:    sigs,
		drain:      5 * time.Second,
		log:        log,
	}
	done := runAsync(r)
	waitFor(t, "HTTP to serve", func() bool { return httpAnswers(addr) })

	sigs <- syscall.SIGINT
	if err := awaitRun(t, done, 3*time.Second); err != nil {
		t.Errorf("clean shutdown returned %v; log = %q", err, log.String())
	}
	if httpAnswers(addr) {
		t.Error("HTTP still serving after shutdown")
	}
	if strings.Contains(log.String(), "error:") {
		t.Errorf("clean shutdown logged an error: %q", log.String())
	}
}

// A component that finishes early without error has nothing left to do;
// it must not take the daemon down.
func TestComponentRunner_EarlyNilExitKeepsServing(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	idle := make(chan struct{})
	r := &componentRunner{
		components: []component{
			{name: "watcher", run: func(context.Context) error { close(idle); return nil }},
			blockUntilCancelled("workers"),
		},
		signals: sigs,
		drain:   5 * time.Second,
		log:     &syncBuffer{},
	}
	done := runAsync(r)
	<-idle
	select {
	case err := <-done:
		t.Fatalf("serve exited (%v) after a component finished cleanly", err)
	case <-time.After(100 * time.Millisecond):
	}
	sigs <- syscall.SIGTERM
	if err := awaitRun(t, done, 2*time.Second); err != nil {
		t.Errorf("clean shutdown returned %v", err)
	}
}

// Cancelling the parent context is a stop request like a signal.
func TestComponentRunner_ParentCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &componentRunner{
		components: []component{blockUntilCancelled("workers")},
		signals:    make(chan os.Signal),
		drain:      5 * time.Second,
		log:        &syncBuffer{},
	}
	done := make(chan error, 1)
	go func() { done <- r.run(ctx) }()
	cancel()
	if err := awaitRun(t, done, 2*time.Second); err != nil {
		t.Errorf("parent cancel returned %v", err)
	}
}
