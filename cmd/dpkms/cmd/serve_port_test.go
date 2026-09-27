package cmd

import (
	"bytes"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// freePort returns a loopback port that was free a moment ago. Tests use
// it as a "preferred" port; every port comes from the OS ephemeral range,
// never a port a live dpkms listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	port := listenerPort(ln)
	_ = ln.Close()
	return port
}

// holdPort binds a loopback port for the rest of the test.
func holdPort(t *testing.T, bind string) int {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, "0"))
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return listenerPort(ln)
}

func TestListenTCP_PreferredAvailable(t *testing.T) {
	port := freePort(t)
	for _, policy := range []portPolicy{portExact, portPreferred} {
		ln, err := listenTCP("127.0.0.1", port, policy)
		if err != nil {
			t.Fatalf("policy %d: listenTCP: %v", policy, err)
		}
		if got := listenerPort(ln); got != port {
			t.Errorf("policy %d: bound %d, want %d", policy, got, port)
		}
		_ = ln.Close()
	}
}

// An operator-set port is bound exactly or serve fails: never a random
// port the operator's clients do not know about.
func TestListenTCP_ExactPortBusyFails(t *testing.T) {
	busy := holdPort(t, "127.0.0.1")

	ln, err := listenTCP("127.0.0.1", busy, portExact)
	if err == nil {
		got := listenerPort(ln)
		_ = ln.Close()
		t.Fatalf("explicit port %d is busy, yet serve bound %d instead of failing", busy, got)
	}
	if !strings.Contains(err.Error(), "address already in use") {
		t.Errorf("error should name the bind failure, got %v", err)
	}
}

func TestListenTCP_DefaultPortBusyFallsBack(t *testing.T) {
	busy := holdPort(t, "127.0.0.1")

	ln, err := listenTCP("127.0.0.1", busy, portPreferred)
	if err != nil {
		t.Fatalf("listenTCP: %v", err)
	}
	defer ln.Close()
	if got := listenerPort(ln); got == busy || got <= 0 {
		t.Errorf("expected an OS-assigned fallback port, got %d (busy %d)", got, busy)
	}
}

func TestListenTCP_BindsRequestedAddress(t *testing.T) {
	// A port held on 0.0.0.0 is busy for a 0.0.0.0 bind.
	busy := holdPort(t, "0.0.0.0")

	ln, err := listenTCP("0.0.0.0", busy, portPreferred)
	if err != nil {
		t.Fatalf("listenTCP: %v", err)
	}
	defer ln.Close()
	if got := listenerPort(ln); got == busy {
		t.Errorf("port %d is busy on 0.0.0.0 yet was bound again", busy)
	}
}

// Two instances started at the same moment race for one preferred port.
// Each must come away holding a live listener, on distinct ports, with
// exactly one of them on the preferred port. A probe-then-rebind
// acquisition tells both the port is free and one loses the later bind.
func TestListenTCP_ConcurrentAcquisitionsOnOnePreferredPort(t *testing.T) {
	const (
		rounds    = 200
		instances = 2
	)
	for round := 0; round < rounds; round++ {
		preferred := freePort(t)

		var (
			wg    sync.WaitGroup
			start = make(chan struct{})
			lns   = make([]net.Listener, instances)
			errs  = make([]error, instances)
		)
		for i := 0; i < instances; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				lns[i], errs[i] = listenTCP("127.0.0.1", preferred, portPreferred)
			}(i)
		}
		close(start)
		wg.Wait()

		onPreferred := 0
		seen := map[int]bool{}
		var failed error
		for i := 0; i < instances; i++ {
			if errs[i] != nil {
				failed = errs[i]
				continue
			}
			p := listenerPort(lns[i])
			if seen[p] {
				t.Errorf("round %d: two instances hold port %d", round, p)
			}
			seen[p] = true
			if p == preferred {
				onPreferred++
			}
		}
		closeListeners(nonNil(lns))
		if failed != nil {
			t.Fatalf("round %d: an instance lost the race for preferred port %d: %v", round, preferred, failed)
		}
		if onPreferred != 1 {
			t.Fatalf("round %d: %d instances on preferred port %d, want exactly 1", round, onPreferred, preferred)
		}
	}
}

func nonNil(lns []net.Listener) []net.Listener {
	out := lns[:0:0]
	for _, ln := range lns {
		if ln != nil {
			out = append(out, ln)
		}
	}
	return out
}

// A refused start releases every port it already bound, so the
// supervisor's restart does not trip over its own previous attempt.
func TestAcquireListeners_ExplicitBusyFailsAndReleasesOthers(t *testing.T) {
	first := freePort(t)
	busy := holdPort(t, "127.0.0.1")

	var log bytes.Buffer
	lns, err := acquireListeners(&log, []listenSpec{
		{name: "HTTP", bind: "127.0.0.1", port: first},
		{name: "gRPC", bind: "127.0.0.1", port: busy, explicit: true},
	})
	if err == nil {
		closeListeners(lns)
		t.Fatal("explicit gRPC port is busy, yet acquireListeners succeeded")
	}
	if !strings.Contains(err.Error(), "gRPC port") {
		t.Errorf("error should name the listener, got %v", err)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(first)))
	if err != nil {
		t.Fatalf("port %d still held after a refused start: %v", first, err)
	}
	_ = ln.Close()
}

func TestAcquireListeners_DefaultBusyIsAnnounced(t *testing.T) {
	busy := holdPort(t, "127.0.0.1")

	var log bytes.Buffer
	lns, err := acquireListeners(&log, []listenSpec{
		{name: "cookie bridge", bind: "127.0.0.1", port: busy},
	})
	if err != nil {
		t.Fatalf("acquireListeners: %v", err)
	}
	defer closeListeners(lns)
	got := listenerPort(lns[0])
	want := "cookie bridge default port " + strconv.Itoa(busy) + " is in use; bound " + strconv.Itoa(got)
	if !strings.Contains(log.String(), want) {
		t.Errorf("fallback not announced; log = %q, want it to contain %q", log.String(), want)
	}
}
