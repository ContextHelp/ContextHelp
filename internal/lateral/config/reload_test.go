package config

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReloadable_SnapshotReturnsCurrent(t *testing.T) {
	r, err := NewReloadable(LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cfg := r.Snapshot()
	if cfg.Scoring.Weights.SessionTopic != 0.5 {
		t.Fatalf("default snapshot weights wrong: %v", cfg.Scoring.Weights.SessionTopic)
	}
}

func TestReloadable_SIGHUPPicksUpMutableEdit(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "lateral.yaml")
	if err := os.WriteFile(proj, []byte("scoring:\n  weights:\n    session_topic: 0.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewReloadable(LoadOptions{ProjectConfigPath: proj})
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshot().Scoring.Weights.SessionTopic != 0.4 {
		t.Fatalf("initial: got %v", r.Snapshot().Scoring.Weights.SessionTopic)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WatchSignal(ctx, r) }()

	// Wait for the watcher to register before sending the signal.
	time.Sleep(50 * time.Millisecond)

	// Edit the file then raise SIGHUP.
	if err := os.WriteFile(proj, []byte("scoring:\n  weights:\n    session_topic: 0.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	// Poll for the snapshot to update.
	deadline := time.After(2 * time.Second)
	for r.Snapshot().Scoring.Weights.SessionTopic != 0.9 {
		select {
		case <-deadline:
			t.Fatalf("snapshot did not update after SIGHUP: still %v", r.Snapshot().Scoring.Weights.SessionTopic)
		case <-time.After(20 * time.Millisecond):
		}
	}

	cancel()
	<-done
}

func TestReloadable_ImmutableChangeVetoed(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "lateral.yaml")
	if err := os.WriteFile(proj, []byte("jobs:\n  engine_kind: memory\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := NewReloadable(LoadOptions{ProjectConfigPath: proj})
	if err != nil {
		t.Fatal(err)
	}
	if r.Snapshot().Jobs.EngineKind != "memory" {
		t.Fatalf("initial: got %q", r.Snapshot().Jobs.EngineKind)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- WatchSignal(ctx, r) }()
	time.Sleep(50 * time.Millisecond)

	// Edit immutable field then SIGHUP — kit should veto, snapshot stays.
	if err := os.WriteFile(proj, []byte("jobs:\n  engine_kind: sqlite\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond) // let the watcher attempt the swap
	if r.Snapshot().Jobs.EngineKind != "memory" {
		t.Fatalf("immutable change should have been vetoed; got engine_kind=%q", r.Snapshot().Jobs.EngineKind)
	}

	cancel()
	<-done
}
