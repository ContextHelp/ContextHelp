package cmd

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// The duplicates config reaches the pipeline registry the service ingests
// through: its threshold and policy land in BuildOpts, and check_similar
// puts dedup in every embedding pipeline.
func TestNewServiceWiresDuplicatesConfig(t *testing.T) {
	db := setupTestDB(t)
	prev := cfg
	t.Cleanup(func() { cfg = prev })
	dup := config.DuplicatesConfig{Policy: "keep", SimilarityThreshold: 0.9, CheckExact: true, CheckSimilar: true}
	cfg = &config.Config{
		Storage:    config.StorageConfig{Type: "sqlite", Path: filepath.Join(filepath.Dir(db.ConfigPath), "test.db")},
		Duplicates: dup,
	}

	if got := embeddingBuildOpts(db.Driver).Duplicates; got != dup {
		t.Errorf("BuildOpts.Duplicates = %+v, want %+v", got, dup)
	}

	svc, cleanup, err := newService()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	p, err := svc.Pipes.Get("text.long")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, s := range p.Steps {
		names = append(names, s.Name())
	}
	if !slices.Contains(names, "dedup") {
		t.Errorf("text.long steps %v: dedup not wired", names)
	}
}
