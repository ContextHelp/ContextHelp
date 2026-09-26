package http

import (
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

func TestNewUpgradeSnapshot(t *testing.T) {
	if got := NewUpgradeSnapshot(upgrade.Status{State: upgrade.StateIdle}); got != nil {
		t.Errorf("idle snapshot = %+v, want nil", got)
	}
	started := time.Date(2026, 9, 26, 12, 0, 0, 0, time.FixedZone("x", 3600))
	got := NewUpgradeSnapshot(upgrade.Status{
		State: upgrade.StateInProgress, Bucket: upgrade.BucketEmbeddingsMigrate, Target: "m@1",
		Progress: 0.5, Done: 2, Total: 4, Failed: 1, EtaSeconds: 3, StartedAt: started,
	})
	want := UpgradeSnapshot{
		State: "in_progress", Bucket: "embeddings_migrate", Target: "m@1",
		Progress: 0.5, Done: 2, Total: 4, Failed: 1, EtaSeconds: 3, StartedAt: "2026-09-26T11:00:00Z",
	}
	if got == nil || *got != want {
		t.Errorf("snapshot = %+v, want %+v", got, want)
	}
}
