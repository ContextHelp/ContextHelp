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

	got, err := findFreePort("127.0.0.1", port)
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

	got, err := findFreePort("127.0.0.1", busy)
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

func TestFindFreePort_ProbesRequestedBindAddress(t *testing.T) {
	// A port held on 0.0.0.0 is busy for a 0.0.0.0 probe. The old
	// implementation probed 127.0.0.1 regardless of the serve bind
	// address and could hand back a port the real bind then lost.
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	defer ln.Close()
	busy := ln.Addr().(*net.TCPAddr).Port

	got, err := findFreePort("0.0.0.0", busy)
	if err != nil {
		t.Fatalf("findFreePort: %v", err)
	}
	if got == busy {
		t.Errorf("port %d is busy on 0.0.0.0 yet was returned as free", busy)
	}
}
