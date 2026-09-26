package upgrade

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// An embedding migration names its target model and counts per-object
// failures; both reach the shadow file the CLI banner reads.
func TestManager_TargetAndFailedCount(t *testing.T) {
	shadow := filepath.Join(t.TempDir(), "upgrade-state.json")
	m := NewManager(shadow)

	if err := m.StartTarget(BucketEmbeddingsMigrate, "nomic@1", 4); err != nil {
		t.Fatal(err)
	}
	if err := m.TickFailed(3, 1); err != nil {
		t.Fatal(err)
	}
	want := Status{State: StateInProgress, Bucket: BucketEmbeddingsMigrate, Target: "nomic@1", Done: 3, Total: 4, Failed: 1, Progress: 0.75}
	if got := counts(m.Snapshot()); got != want {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	disk, _, err := ReadShadow(shadow)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Target != "nomic@1" || disk.Failed != 1 {
		t.Errorf("shadow = %+v, want target and failed count", disk)
	}

	// A failed run keeps the target and the counts for the operator.
	if err := m.Fail(errors.New("1 object not embedded")); err != nil {
		t.Fatal(err)
	}
	want.State, want.LastError = StateFailed, "1 object not embedded"
	if got := counts(m.Snapshot()); got != want {
		t.Errorf("after Fail = %+v, want %+v", got, want)
	}

	// Start (no target) resets both fields.
	if err := m.Start(BucketReingestSelective, 1); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot(); got.Target != "" || got.Failed != 0 {
		t.Errorf("fresh run kept %+v", got)
	}
}

func TestManager_TickFailedRejectsBadCounts(t *testing.T) {
	m := NewManager("")
	if err := m.TickFailed(1, 0); !errors.Is(err, ErrNotRunning) {
		t.Errorf("TickFailed while idle = %v, want ErrNotRunning", err)
	}
	if err := m.StartTarget(BucketEmbeddingsMigrate, "m@1", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.TickFailed(1, 2); err == nil {
		t.Error("failed > done accepted")
	}
	if err := m.TickFailed(1, -1); err == nil {
		t.Error("negative failed accepted")
	}
}

// counts drops the time-derived fields so a Status compares by value.
func counts(s Status) Status {
	s.StartedAt = time.Time{}
	s.EtaSeconds = 0
	return s
}
