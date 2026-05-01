package federation

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// fakePusher is a Pusher for unit tests of WorkerSet.
// It records every Push call (count + last objects) and can fail on demand.
type fakePusher struct {
	mu       sync.Mutex
	name     string
	calls    int32
	pushed   []storage.KnowledgeObject
	failNext atomic.Int32 // when >0, Push returns err and decrements
	err      error
}

func (p *fakePusher) Name() string { return p.name }

func (p *fakePusher) Push(_ context.Context, objects []storage.KnowledgeObject, _ []storage.Edge, _ []storage.Entity) error {
	atomic.AddInt32(&p.calls, 1)
	if remaining := p.failNext.Load(); remaining > 0 {
		p.failNext.Store(remaining - 1)
		return p.err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pushed = append(p.pushed, objects...)
	return nil
}

func (p *fakePusher) Calls() int32 { return atomic.LoadInt32(&p.calls) }

func (p *fakePusher) PushedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pushed)
}

// newSourceDB creates a SQLite DB with one object so Push has something to send.
func newSourceDB(t *testing.T, ids ...string) (storage.StorageDriver, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.db")
	drv, err := storageutil.NewDriver("sqlite", path)
	if err != nil {
		t.Fatalf("newSourceDB: %v", err)
	}
	if err := drv.Init(context.Background()); err != nil {
		t.Fatalf("newSourceDB init: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })

	for _, id := range ids {
		obj := storage.KnowledgeObject{
			ID:          id,
			Type:        "note",
			RawContent:  "content-" + id,
			ContentHash: "hash-" + id,
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}
		if err := drv.Objects().Create(context.Background(), &obj); err != nil {
			t.Fatalf("seed object %q: %v", id, err)
		}
	}
	return drv, path
}

// TestFederationWorker_TicksAndPushes verifies a worker invokes its Pusher
// at least once per Interval tick when objects are available in source.
func TestFederationWorker_TicksAndPushes(t *testing.T) {
	src, _ := newSourceDB(t, "obj-1")
	pusher := &fakePusher{name: "fed-1"}

	ws, err := newWorkerSetWithPushers(t, src, []FederationTarget{
		{Name: "fed-1", URL: "/tmp/ignored.db", SyncMode: "async", Interval: 50 * time.Millisecond},
	}, map[string]Pusher{"fed-1": pusher})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws.Start(ctx)

	// Wait for at least one tick (50ms interval) → call recorded.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pusher.Calls() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pusher.Calls() < 1 {
		t.Fatalf("worker did not Push within 2s: calls=%d", pusher.Calls())
	}
	cancel()
	if err := ws.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// TestFederationWorker_ContextCancelExitsWithin5s verifies AC #9: workers
// honour ctx.Done and Stop returns within 5s.
func TestFederationWorker_ContextCancelExitsWithin5s(t *testing.T) {
	src, _ := newSourceDB(t, "obj-cancel")
	pusher := &fakePusher{name: "fed-cancel"}

	ws, err := newWorkerSetWithPushers(t, src, []FederationTarget{
		{Name: "fed-cancel", URL: "/tmp/ignored.db", SyncMode: "async", Interval: time.Hour},
	}, map[string]Pusher{"fed-cancel": pusher})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ws.Start(ctx)
	cancel()

	stopDone := make(chan error, 1)
	go func() { stopDone <- ws.Stop(context.Background()) }()

	select {
	case err := <-stopDone:
		if err != nil {
			t.Errorf("Stop returned err: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not exit within 5s after context cancel")
	}
}

// TestFederationWorker_ContinuesAfterPushError verifies AC #8: a failing Push
// is logged and the worker keeps ticking; the next successful tick records
// the object.
func TestFederationWorker_ContinuesAfterPushError(t *testing.T) {
	src, _ := newSourceDB(t, "obj-retry")
	pusher := &fakePusher{name: "fed-retry", err: errors.New("transient")}
	pusher.failNext.Store(1) // first Push fails; subsequent ones succeed.

	ws, err := newWorkerSetWithPushers(t, src, []FederationTarget{
		{Name: "fed-retry", URL: "/tmp/ignored.db", SyncMode: "async", Interval: 30 * time.Millisecond},
	}, map[string]Pusher{"fed-retry": pusher})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws.Start(ctx)

	// Need >= 2 calls (one fail + at least one success).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pusher.Calls() >= 2 && pusher.PushedCount() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pusher.Calls() < 2 {
		t.Errorf("want >=2 calls (1 fail + retry), got %d", pusher.Calls())
	}
	if pusher.PushedCount() < 1 {
		t.Errorf("want at least 1 successful push, got %d", pusher.PushedCount())
	}
	cancel()
	_ = ws.Stop(context.Background())
}

// TestFederationWorker_StartupCycleDetected verifies US-0318 cycle detection:
// when a federation target points back to the source DB path, New() returns
// an error including the cycle path.
func TestFederationWorker_StartupCycleDetected(t *testing.T) {
	src, srcPath := newSourceDB(t)

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{Name: "loop", URL: srcPath, SyncMode: "async", Interval: time.Minute},
		},
	}
	cfg.Storage.Path = srcPath

	_, err := New(cfg, src)
	if err == nil {
		t.Fatal("New: expected cycle error, got nil")
	}
	// Error message must reference cycle so operators can act.
	if !contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err.Error())
	}
}

// TestFederationWorker_NoTargetsNoOp verifies AC: zero federations entries
// = isolated instance; no goroutines, Stop returns nil immediately.
func TestFederationWorker_NoTargetsNoOp(t *testing.T) {
	src, srcPath := newSourceDB(t)

	cfg := config.Config{Federations: nil}
	cfg.Storage.Path = srcPath

	ws, err := New(cfg, src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n := ws.Len(); n != 0 {
		t.Errorf("want 0 workers, got %d", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ws.Start(ctx)
	if err := ws.Stop(context.Background()); err != nil {
		t.Errorf("Stop on empty set: %v", err)
	}
}

// TestFederationWorker_SkipsInlineTargets verifies that inline-mode targets
// do NOT spawn async workers (Phase 1: inline isn't implemented; treated
// as no-op for worker spawning).
func TestFederationWorker_SkipsInlineTargets(t *testing.T) {
	src, srcPath := newSourceDB(t)

	cfg := config.Config{
		Federations: []config.FederationEntry{
			{Name: "inline-only", URL: "/tmp/inline.db", SyncMode: "inline"},
		},
	}
	cfg.Storage.Path = srcPath

	ws, err := New(cfg, src)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n := ws.Len(); n != 0 {
		t.Errorf("inline target spawned a worker: want 0, got %d", n)
	}
}

// contains is a tiny helper so we don't import strings just for substring.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// newWorkerSetWithPushers builds a WorkerSet with injected Pushers (test-only).
// Bypasses Pusher construction from URL so unit tests don't write real files.
func newWorkerSetWithPushers(
	t *testing.T,
	src storage.StorageDriver,
	targets []FederationTarget,
	pushers map[string]Pusher,
) (*WorkerSet, error) {
	t.Helper()
	ws := &WorkerSet{src: src}
	for _, tgt := range targets {
		p, ok := pushers[tgt.Name]
		if !ok {
			t.Fatalf("missing pusher for %q", tgt.Name)
		}
		ws.workers = append(ws.workers, &Worker{target: tgt, src: src, pusher: p})
	}
	return ws, nil
}
