package upgrade

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestManagerLifecycleCompletes drives the happy path:
// idle → Start → Tick → Tick → Complete → idle (shadow removed).
func TestManagerLifecycleCompletes(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	m := NewManager(shadow)

	// Idle has no shadow file.
	if _, err := os.Stat(shadow); !os.IsNotExist(err) {
		t.Fatalf("expected no shadow at idle, got err=%v", err)
	}

	// Drive a fixed clock so ETA is deterministic.
	t0 := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	clock := t0
	m.now = func() time.Time { return clock }

	if err := m.Start(BucketReingestSelective, 100); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := m.Snapshot().State; got != StateInProgress {
		t.Fatalf("want in_progress, got %q", got)
	}
	if _, err := os.Stat(shadow); err != nil {
		t.Fatalf("shadow should exist after Start: %v", err)
	}

	// 10 seconds in, 25 / 100 done — expect ~30s ETA.
	clock = t0.Add(10 * time.Second)
	if err := m.Tick(25); err != nil {
		t.Fatalf("Tick(25): %v", err)
	}
	snap := m.Snapshot()
	if snap.Done != 25 || snap.Total != 100 {
		t.Fatalf("Tick should set done/total: %+v", snap)
	}
	if snap.Progress < 0.24 || snap.Progress > 0.26 {
		t.Fatalf("Tick progress: want ~0.25, got %f", snap.Progress)
	}
	if snap.EtaSeconds < 28 || snap.EtaSeconds > 32 {
		t.Fatalf("Tick ETA: want ~30, got %d", snap.EtaSeconds)
	}

	if err := m.Complete(); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := m.Snapshot().State; got != StateIdle {
		t.Fatalf("want idle after Complete, got %q", got)
	}
	if _, err := os.Stat(shadow); !os.IsNotExist(err) {
		t.Fatalf("shadow must be removed after Complete, err=%v", err)
	}
}

// TestManagerFailRetainsShadow confirms a failed run keeps the shadow file
// (so the CLI banner keeps reminding) and records LastError.
func TestManagerFailRetainsShadow(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	m := NewManager(shadow)

	if err := m.Start(BucketReindexAuto, 50); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Fail(errors.New("disk full")); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	snap := m.Snapshot()
	if snap.State != StateFailed {
		t.Fatalf("want failed, got %q", snap.State)
	}
	if snap.LastError != "disk full" {
		t.Fatalf("want LastError=disk full, got %q", snap.LastError)
	}
	if snap.Bucket != BucketReindexAuto {
		t.Fatalf("Fail should retain bucket, got %q", snap.Bucket)
	}
	// Shadow persists.
	if _, err := os.Stat(shadow); err != nil {
		t.Fatalf("shadow should persist after Fail: %v", err)
	}
}

// TestManagerStartRejectsConcurrentRuns asserts a second Start while the
// first is in flight returns an error rather than corrupting the state.
func TestManagerStartRejectsConcurrentRuns(t *testing.T) {
	m := NewManager("")
	if err := m.Start(BucketReingestSelective, 10); err != nil {
		t.Fatalf("Start: %v", err)
	}
	err := m.Start(BucketReindexAuto, 20)
	if err == nil {
		t.Fatal("second Start should fail")
	}
}

// TestManagerTickRequiresInProgress: Tick on idle / failed returns
// ErrNotRunning.
func TestManagerTickRequiresInProgress(t *testing.T) {
	m := NewManager("")
	if err := m.Tick(1); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Tick on idle: want ErrNotRunning, got %v", err)
	}
	if err := m.Complete(); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Complete on idle: want ErrNotRunning, got %v", err)
	}
}

// TestComputeETAEdgeCases protects against divide-by-zero / negative ETA
// regressions in the linear extrapolation.
func TestComputeETAEdgeCases(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		done     int
		total    int
		started  time.Time
		wantZero bool
	}{
		{"no_progress", 0, 100, now.Add(-5 * time.Second), true},
		{"zero_total", 5, 0, now.Add(-5 * time.Second), true},
		{"complete", 100, 100, now.Add(-5 * time.Second), true},
		{"future_start", 5, 100, now.Add(time.Second), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eta := computeETA(tc.done, tc.total, tc.started, now)
			if tc.wantZero && eta != 0 {
				t.Fatalf("want 0, got %d", eta)
			}
		})
	}
}

// TestReadShadowRoundtrip writes a Status to disk and reads it back,
// then exercises the staleness gate.
func TestReadShadowRoundtrip(t *testing.T) {
	dir := t.TempDir()
	shadow := filepath.Join(dir, "upgrade-state.json")
	m := NewManager(shadow)

	t0 := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return t0 }

	if err := m.Start(BucketReingestSelective, 120); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.now = func() time.Time { return t0.Add(10 * time.Second) }
	if err := m.Tick(47); err != nil {
		t.Fatalf("Tick: %v", err)
	}

	got, modTime, err := ReadShadow(shadow)
	if err != nil {
		t.Fatalf("ReadShadow: %v", err)
	}
	if got.State != StateInProgress || got.Bucket != BucketReingestSelective {
		t.Fatalf("ReadShadow shape mismatch: %+v", got)
	}
	if got.Done != 47 || got.Total != 120 {
		t.Fatalf("ReadShadow done/total: %+v", got)
	}

	// ReadShadowFresh: still fresh.
	st, ok, err := ReadShadowFresh(shadow, modTime.Add(time.Second))
	if err != nil {
		t.Fatalf("ReadShadowFresh fresh: %v", err)
	}
	if !ok {
		t.Fatal("expected fresh=true")
	}
	if st.Done != 47 {
		t.Fatalf("fresh content: %+v", st)
	}

	// ReadShadowFresh: stale (mod time + StaleAfter + 1s).
	_, ok, err = ReadShadowFresh(shadow, modTime.Add(ShadowStaleAfter+time.Second))
	if err != nil {
		t.Fatalf("ReadShadowFresh stale: %v", err)
	}
	if ok {
		t.Fatal("expected stale=false past ShadowStaleAfter")
	}

	// Missing file maps to (zero, false, nil).
	missing := filepath.Join(dir, "no-such.json")
	_, ok, err = ReadShadowFresh(missing, time.Now())
	if err != nil {
		t.Fatalf("ReadShadowFresh missing: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false on missing file")
	}
}
