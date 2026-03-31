package cmd

import (
	"net"
	"testing"
)

func TestFindFreePort_PreferredAvailable(t *testing.T) {
	// Grab a free port then release it — findFreePort should reclaim it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	got, err := findFreePort(port)
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	if got != port {
		t.Errorf("preferred port available: got %d, want %d", got, port)
	}
}

func TestFindFreePort_PreferredBusy(t *testing.T) {
	// Hold a port open so findFreePort must fall back.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	got, err := findFreePort(busy)
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	if got == busy {
		t.Errorf("expected fallback port, got same busy port %d", busy)
	}
	if got <= 0 {
		t.Errorf("expected valid port, got %d", got)
	}
}
