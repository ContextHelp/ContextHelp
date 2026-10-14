package storagetest

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ReinforceReindexConformance pins that Reinforce, the path re-ingest of
// duplicate content takes, re-indexes the object like Update: the text
// and tags it writes are what full-text search finds, and the text it
// replaced is no longer found.
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
		ID: "reinforce-reindex", Type: "note", RawContent: "blob://feedbeef",
		TextContent: "aardvark burrow", ContentHash: hash, ReinforcementCount: 1,
		CreatedAt: fixtureTime(), UpdatedAt: fixtureTime(),
	}
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := search(t, "aardvark"); len(got) != 1 {
		t.Fatalf("precondition: search aardvark = %v, want [%s]", got, obj.ID)
	}

	id, err := drv.Objects().Reinforce(ctx, hash, &storage.KnowledgeObject{
		RawContent: obj.RawContent, TextContent: "pangolin scales",
	})
	if err != nil {
		t.Fatalf("Reinforce: %v", err)
	}
	if id != obj.ID {
		t.Fatalf("Reinforce = %q, want %q", id, obj.ID)
	}
	if got := search(t, "pangolin"); len(got) != 1 || got[0] != obj.ID {
		t.Errorf("search for the reinforced text = %v, want [%s]: Reinforce did not re-index", got, obj.ID)
	}
	if got := search(t, "aardvark"); len(got) != 0 {
		t.Errorf("search for the replaced text = %v, want none: the old index entry survived", got)
	}
	got, err := drv.Objects().Get(ctx, obj.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TextContent != "pangolin scales" {
		t.Errorf("stored TextContent = %q, want the reinforced text", got.TextContent)
	}
	if !got.FTSIndexed {
		t.Error("fts_indexed cleared on a reinforced, indexed object")
	}
}
