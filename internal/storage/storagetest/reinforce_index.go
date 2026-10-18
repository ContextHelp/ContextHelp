package storagetest

import (
	"context"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ReinforceReindexConformance pins Reinforce, the path re-ingest of
// duplicate content takes: it bumps the reinforcement count and merges
// tags, and never replaces the stored content. A duplicate is found by
// the folded content hash, so the newer bytes may differ in case or
// spacing; the object keeps its first bytes. The row is
// re-indexed like Update indexes it: the stored text stays found, the
// duplicate's is never indexed.
func ReinforceReindexConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	ctx := context.Background()
	search := func(t *testing.T, q string) []string {
		t.Helper()
		res, err := drv.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 10})
		if err != nil {
			t.Fatalf("FTSSearch %q: %v", q, err)
		}
		ids := make([]string, 0, len(res))
		for _, o := range res {
			ids = append(ids, o.ID)
		}
		return ids
	}
	const hash = "reinforce-reindex-hash"
	obj := &storage.KnowledgeObject{
		ID: "reinforce-reindex", Type: "note", RawContent: "aardvark burrow",
		TextContent: "aardvark burrow", ContentHash: hash, ReinforcementCount: 1,
		Tags:      []storage.Tag{{Label: "first"}},
		CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := search(t, "aardvark"); len(got) != 1 {
		t.Fatalf("precondition: search aardvark = %v, want [%s]", got, obj.ID)
	}

	id, err := drv.Objects().Reinforce(ctx, hash, &storage.KnowledgeObject{
		RawContent: "AARDVARK   pangolin", TextContent: "pangolin scales",
		Tags: []storage.Tag{{Label: "badgerly"}},
	})
	if err != nil {
		t.Fatalf("Reinforce: %v", err)
	}
	if id != obj.ID {
		t.Fatalf("Reinforce = %q, want %q", id, obj.ID)
	}
	got, err := drv.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.RawContent != obj.RawContent || got.TextContent != obj.TextContent {
		t.Errorf("stored content = %q / %q, want the first bytes %q: Reinforce replaced it", got.RawContent, got.TextContent, obj.RawContent)
	}
	if got.ReinforcementCount != 2 || got.LastReinforcedAt == nil {
		t.Errorf("reinforcement = %d, %v; want 2 and a time", got.ReinforcementCount, got.LastReinforcedAt)
	}
	labels := make([]string, 0, len(got.Tags))
	for _, tag := range got.Tags {
		labels = append(labels, tag.Label)
	}
	if !slices.Contains(labels, "first") || !slices.Contains(labels, "badgerly") {
		t.Errorf("tags = %v, want first and badgerly merged", labels)
	}
	if got := search(t, "aardvark"); len(got) != 1 || got[0] != obj.ID {
		t.Errorf("search for the stored text = %v, want [%s]", got, obj.ID)
	}
	if got := search(t, "pangolin"); len(got) != 0 {
		t.Errorf("search for the duplicate's text = %v, want none: it was never stored", got)
	}
	if !got.FTSIndexed {
		t.Error("fts_indexed cleared on a reinforced, indexed object")
	}
}
