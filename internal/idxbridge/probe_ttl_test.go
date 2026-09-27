package idxbridge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

// switchableHealth serves /health with a flippable status.
type switchableHealth struct {
	up    atomic.Bool
	calls atomic.Int64
}

func (s *switchableHealth) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		if s.up.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	return mux
}

func TestProbe_NegativeResultRecheckedQuickly(t *testing.T) {
	// Scenario: CLI probes just before daemon startup. A symmetric 30 s
	// cache would keep it on the direct path long after the daemon is up;
	// the negative TTL must be short.
	sh := &switchableHealth{}
	srv := httptest.NewServer(sh.handler())
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:               srv.URL,
		ProbeTimeout:          time.Second,
		ProbeInterval:         time.Minute,           // daemon-up cached long
		NegativeProbeInterval: 50 * time.Millisecond, // daemon-down cached short
		Fallback:              fb,
		HTTPClient:            srv.Client(),
	})

	ctx := context.Background()

	// Daemon not up yet → negative probe.
	if bridge.Probe(ctx) {
		t.Fatal("Probe true while daemon down")
	}
	// Daemon comes up; within the negative TTL the cached miss still holds.
	sh.up.Store(true)
	if bridge.Probe(ctx) {
		t.Fatal("Probe true inside negative TTL; want cached miss")
	}
	if got := sh.calls.Load(); got != 1 {
		t.Fatalf("health called %d times inside negative TTL; want 1", got)
	}

	// After the negative TTL the bridge must re-probe and see the daemon.
	deadline := time.Now().Add(2 * time.Second)
	for !bridge.Probe(ctx) {
		if time.Now().After(deadline) {
			t.Fatal("Probe still false after negative TTL elapsed; negative result cached too long")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProbe_PositiveResultCachedLong(t *testing.T) {
	sh := &switchableHealth{}
	sh.up.Store(true)
	srv := httptest.NewServer(sh.handler())
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:               srv.URL,
		ProbeTimeout:          time.Second,
		ProbeInterval:         time.Minute,
		NegativeProbeInterval: 10 * time.Millisecond,
		Fallback:              fb,
		HTTPClient:            srv.Client(),
	})

	ctx := context.Background()
	if !bridge.Probe(ctx) {
		t.Fatal("Probe false for live daemon")
	}

	// Daemon flips down; the positive result stays cached for ProbeInterval —
	// the short TTL applies to negative results only.
	sh.up.Store(false)
	time.Sleep(30 * time.Millisecond) // well past the negative TTL
	if !bridge.Probe(ctx) {
		t.Fatal("positive probe result expired early; want ProbeInterval cache")
	}
	if got := sh.calls.Load(); got != 1 {
		t.Fatalf("health called %d times inside positive TTL; want 1", got)
	}
}

func TestDefaultNegativeProbeInterval_ShorterThanPositive(t *testing.T) {
	if idxbridge.DefaultNegativeProbeInterval >= idxbridge.DefaultProbeInterval {
		t.Fatalf("DefaultNegativeProbeInterval (%v) must be shorter than DefaultProbeInterval (%v)",
			idxbridge.DefaultNegativeProbeInterval, idxbridge.DefaultProbeInterval)
	}
	if idxbridge.DefaultNegativeProbeInterval > 2*time.Second {
		t.Fatalf("DefaultNegativeProbeInterval = %v; want ~1s so a fresh daemon is noticed quickly",
			idxbridge.DefaultNegativeProbeInterval)
	}
}
