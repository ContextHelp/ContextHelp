package ws

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestCookieBridgeServe_ReturnsListenerFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = ln.Close()

	srv := NewCookieBridgeServer(NewCookieCache())
	if err := srv.Serve(context.Background(), ln); err == nil {
		t.Fatal("Serve on a dead listener returned nil; the failure must reach the caller")
	}
}

func TestCookieBridgeServe_StopsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- NewCookieBridgeServer(NewCookieCache()).Serve(ctx, ln) }()

	// Serving on the listener we handed over, not a re-bound port.
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve after cancel: %v", err)
		}
	case <-time.After(cookieBridgeShutdownTimeout + time.Second):
		t.Fatal("Serve did not return after cancel")
	}
	if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
		_ = c.Close()
		t.Error("listener still accepting after Serve returned")
	}
}
