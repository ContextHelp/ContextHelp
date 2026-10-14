package security

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// gatedAudit blocks every Append until release is closed (or the
// write's context ends, in which case nothing is recorded). entered
// receives one value per Append call, before it blocks.
type gatedAudit struct {
	stubAudit
	entered chan struct{}
	release chan struct{}

	mu      sync.Mutex
	aborted int
}

func newGatedAudit() *gatedAudit {
	return &gatedAudit{entered: make(chan struct{}, 64), release: make(chan struct{})}
}

func (g *gatedAudit) Append(ctx context.Context, e *storage.AuditEntry) error {
	g.entered <- struct{}{}
	select {
	case <-g.release:
		return g.stubAudit.Append(ctx, e)
	case <-ctx.Done():
		g.mu.Lock()
		g.aborted++
		g.mu.Unlock()
		return ctx.Err()
	}
}

func waitEntered(t *testing.T, g *gatedAudit) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("audit Append never called")
	}
}

// immediateCfg fires an alert on every auth failure.
func immediateCfg(queue int) Config {
	cfg := DefaultConfig()
	cfg.AuthFailureThreshold = 1
	cfg.AuditQueueSize = queue
	return cfg
}

// Close drains every queued audit row before returning, so a store
// closed right after it holds all of them.
func TestEmitter_CloseDrainsPendingAuditRows(t *testing.T) {
	audit := newGatedAudit()
	e := New(immediateCfg(16), audit)

	const n = 5
	for i := 0; i < n; i++ {
		e.RecordAuthFailure(context.Background(), "user:drain")
	}
	waitEntered(t, audit) // first row in flight, the rest queued

	closed := make(chan error, 1)
	go func() { closed <- e.Close(context.Background()) }()

	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) before the queued rows were written", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(audit.release)

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the store was released")
	}
	if got := len(audit.snapshot()); got != n {
		t.Fatalf("audit rows after Close = %d, want %d", got, n)
	}
	if d := e.Dropped(); d != 0 {
		t.Fatalf("Dropped = %d, want 0", d)
	}
}

// A full queue drops the alert's audit row instead of blocking the
// caller, and counts it.
func TestEmitter_FullQueueDropsWithoutBlocking(t *testing.T) {
	audit := newGatedAudit()
	const size = 2
	e := New(immediateCfg(size), audit)
	t.Cleanup(func() { _ = e.Close(context.Background()) })

	e.RecordAuthFailure(context.Background(), "user:flood")
	waitEntered(t, audit) // worker busy with row 1; queue empty

	const extra = 7
	done := make(chan struct{})
	go func() {
		for i := 0; i < size+extra; i++ {
			e.RecordAuthFailure(context.Background(), "user:flood")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		close(audit.release) // unblock the writer so cleanup can finish
		t.Fatal("recording blocked on a full audit queue")
	}
	if d := e.Dropped(); d != extra {
		t.Fatalf("Dropped = %d, want %d", d, extra)
	}

	close(audit.release)
	if err := e.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := len(audit.snapshot()); got != 1+size {
		t.Fatalf("audit rows = %d, want %d (in flight + queued)", got, 1+size)
	}
}

// After Close returns, nothing writes to the store: new alerts are
// refused, and when the deadline cuts the drain short the in-flight
// write is cancelled and the rest discarded.
func TestEmitter_NoWriteAfterClose(t *testing.T) {
	t.Run("record after close", func(t *testing.T) {
		audit := &stubAudit{}
		e := New(immediateCfg(4), audit)
		if err := e.Close(context.Background()); err != nil {
			t.Fatalf("Close: %v", err)
		}
		e.RecordAuthFailure(context.Background(), "user:late")
		time.Sleep(20 * time.Millisecond)
		if got := len(audit.snapshot()); got != 0 {
			t.Fatalf("audit rows after Close = %d, want 0", got)
		}
		if err := e.Close(context.Background()); err != nil {
			t.Fatalf("second Close: %v", err)
		}
	})

	t.Run("deadline cuts drain", func(t *testing.T) {
		audit := newGatedAudit()
		e := New(immediateCfg(8), audit)
		for i := 0; i < 3; i++ {
			e.RecordAuthFailure(context.Background(), "user:slow")
		}
		waitEntered(t, audit)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		if err := e.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Close = %v, want DeadlineExceeded", err)
		}
		close(audit.release)
		time.Sleep(30 * time.Millisecond)
		if got := len(audit.snapshot()); got != 0 {
			t.Fatalf("audit rows written after Close = %d, want 0", got)
		}
		audit.mu.Lock()
		aborted := audit.aborted
		audit.mu.Unlock()
		if aborted != 1 {
			t.Fatalf("aborted in-flight writes = %d, want 1", aborted)
		}
	})
}
