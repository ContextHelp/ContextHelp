package config

import "testing"

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
