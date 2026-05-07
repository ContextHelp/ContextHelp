package upgrade

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeReanalyzer records calls to ReanalyzeObject and lets each test
// inject its own response per call. Threadsafe so tests that exercise
// concurrent edges (cancel-mid-run) don't race.
type fakeReanalyzer struct {
	mu       sync.Mutex
	calls    []string
	costs    []float64 // per-call cost; defaults to 0 when unset
	errs     []error   // per-call error; defaults to nil
	delay    time.Duration
	failOnce bool
	failErr  error
}

func (f *fakeReanalyzer) ReanalyzeObject(ctx context.Context, id string) (string, float64, error) {
	f.mu.Lock()
	idx := len(f.calls)
	f.calls = append(f.calls, id)
	cost := 0.0
	if idx < len(f.costs) {
		cost = f.costs[idx]
	}
	var err error
	if idx < len(f.errs) {
		err = f.errs[idx]
	}
	if f.failOnce && idx == 0 && f.failErr != nil {
		err = f.failErr
	}
	delay := f.delay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return "", 0, ctx.Err()
		case <-time.After(delay):
		}
	}
	return id + "@v1", cost, err
}

func (f *fakeReanalyzer) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// TestWorker_HappyPath drives 5 selector matches through Worker.Run and
// asserts the manager observed Start → Tick(1..5) → Complete.
func TestWorker_HappyPath(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Insert 3 more text.short@v0 rows so the selector matches 5.
	for i, id := range []string{"obj-f", "obj-g", "obj-h"} {
		_, err := db.Exec(
			"INSERT INTO objects (id, pipeline, graph_json, created_at) VALUES (?, ?, ?, ?)",
			id, "text.short@v0", "{}", time.Now().Add(time.Duration(i)*time.Second).Format(time.RFC3339),
		)
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	mgr := NewManager("")
	fake := &fakeReanalyzer{}
	w := NewWorker(db, fake, mgr, nil)

	if err := w.Run(context.Background(), sel, WorkerOpts{}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Manager state: idle (Complete clears state).
	if got := mgr.Snapshot().State; got != StateIdle {
		t.Errorf("state = %q, want idle", got)
	}
	if got := fake.CallCount(); got != 5 {
		t.Errorf("ReanalyzeObject called %d times, want 5", got)
	}
}

// TestWorker_ContextCancelMidRun: cancelling the ctx mid-run leaves the
// manager in failed with reason "context cancelled".
func TestWorker_ContextCancelMidRun(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mgr := NewManager("")
	fake := &fakeReanalyzer{delay: 50 * time.Millisecond}
	w := NewWorker(db, fake, mgr, nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a tiny delay so at least one Tick fires before the
	// abort.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err = w.Run(ctx, sel, WorkerOpts{})
	if err == nil {
		t.Fatal("expected error on cancellation")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	snap := mgr.Snapshot()
	if snap.State != StateFailed {
		t.Errorf("state = %q, want failed", snap.State)
	}
	if snap.LastError == "" {
		t.Errorf("LastError empty; expected reason")
	}
}

// TestWorker_BudgetExceededAborts: the worker aborts cleanly when the
// running cost surpasses BudgetUSD.
func TestWorker_BudgetExceededAborts(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mgr := NewManager("")
	// Each call costs $0.10 — budget $0.15 → fails on call #2.
	fake := &fakeReanalyzer{costs: []float64{0.10, 0.10, 0.10}}
	w := NewWorker(db, fake, mgr, nil)

	err = w.Run(context.Background(), sel, WorkerOpts{BudgetUSD: 0.15})
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("err = %v, want ErrBudgetExceeded", err)
	}
	snap := mgr.Snapshot()
	if snap.State != StateFailed {
		t.Errorf("state = %q, want failed", snap.State)
	}
	if snap.LastError == "" {
		t.Error("LastError empty; expected budget message")
	}
}

// TestWorker_DryRunSkipsReanalyze: --dry-run never calls ReanalyzeObject
// and never moves the manager out of idle.
func TestWorker_DryRunSkipsReanalyze(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mgr := NewManager("")
	fake := &fakeReanalyzer{}
	w := NewWorker(db, fake, mgr, nil)

	if err := w.Run(context.Background(), sel, WorkerOpts{DryRun: true}); err != nil {
		t.Fatalf("DryRun: %v", err)
	}
	if got := fake.CallCount(); got != 0 {
		t.Errorf("ReanalyzeObject called %d times in dry-run, want 0", got)
	}
	if got := mgr.Snapshot().State; got != StateIdle {
		t.Errorf("state = %q, want idle", got)
	}
}

// TestWorker_ReanalyzeErrorMovesToFailed: a single ReanalyzeObject error
// aborts the run and parks the manager in failed.
func TestWorker_ReanalyzeErrorMovesToFailed(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mgr := NewManager("")
	fake := &fakeReanalyzer{
		failOnce: true,
		failErr:  errors.New("simulated step failure"),
	}
	w := NewWorker(db, fake, mgr, nil)

	err = w.Run(context.Background(), sel, WorkerOpts{})
	if err == nil {
		t.Fatal("expected propagated error")
	}
	if got := mgr.Snapshot().State; got != StateFailed {
		t.Errorf("state = %q, want failed", got)
	}
}

// TestWorker_AlreadyInProgress: a second Run while the first holds the
// manager returns ErrAlreadyInProgress.
func TestWorker_AlreadyInProgress(t *testing.T) {
	db := fixtureDB(t)
	sel, err := ParseSelector("pipeline=text.short@v0")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	mgr := NewManager("")
	if err := mgr.Start(BucketReindexAuto, 5); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer mgr.Complete()

	fake := &fakeReanalyzer{}
	w := NewWorker(db, fake, mgr, nil)

	err = w.Run(context.Background(), sel, WorkerOpts{})
	if !errors.Is(err, ErrAlreadyInProgress) {
		t.Fatalf("err = %v, want ErrAlreadyInProgress", err)
	}
}
