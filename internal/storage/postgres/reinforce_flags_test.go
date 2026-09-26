//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestPostgresReinforceKeepsIndexFlags pins that the dedup/reinforcement
// path leaves an already-indexed object searchable AND still reporting
// fts_indexed/vector_indexed=true. Reinforce never touches the tsvector
// (generated from projected_fts_body, which it does not rewrite) or the
// embedding column, and no re-indexer exists to flip the flags back —
// clearing them misreports a searchable object as unindexed. Mirrors the
// SQLite regression fix.
func TestPostgresReinforceKeepsIndexFlags(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	obj := makePgFTSObject("reinf-pg-1", "article", "zebrafish larval locomotion study")
	obj.RawContent = "zebrafish larval locomotion study"
	obj.ContentHash = "reinf-pg-hash"
	obj.ReinforcementCount = 1
	obj.VectorIndexed = true
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	before, err := drv.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if !before.FTSIndexed {
		t.Fatal("precondition: Create must FTS-index the object")
	}
	if !before.VectorIndexed {
		t.Fatal("precondition: Create persists vector_indexed")
	}

	if _, err := drv.Objects().Reinforce(ctx, "reinf-pg-hash", &storage.KnowledgeObject{
		RawContent: obj.RawContent,
		Tags:       []storage.Tag{{Label: "extra", Weight: 1.0, Source: "user"}},
	}); err != nil {
		t.Fatalf("reinforce: %v", err)
	}

	after, err := drv.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if after.ReinforcementCount != 2 {
		t.Errorf("reinforcement_count: got %d want 2", after.ReinforcementCount)
	}
	if !after.FTSIndexed {
		t.Error("Reinforce must not clear fts_indexed: object stays indexed")
	}
	if !after.VectorIndexed {
		t.Error("Reinforce must not clear vector_indexed: embedding untouched")
	}

	results, err := drv.Objects().FTSSearch(ctx, "zebrafish", storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != obj.ID {
		t.Fatalf("reinforced object must remain FTS-searchable: got %v", ids(results))
	}
	if !results[0].FTSIndexed {
		t.Error("search result must report fts_indexed=true")
	}
}
