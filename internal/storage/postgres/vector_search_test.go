//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// TestPostgres_VectorSearch_FilterWithBoundVector guards the placeholder
// layout of VectorSearch: the query vector is bound as a parameter and the
// filter conditions number themselves around it. A mis-numbered placeholder
// surfaces here as a driver error or a wrong result set.
func TestPostgres_VectorSearch_FilterWithBoundVector(t *testing.T) {
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	drv.SetVectorDimension(storagetest.VectorRankDimension)
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	ctx := context.Background()

	mk := func(id, typ, subtype string, emb []float32) {
		t.Helper()
		obj := &pluginapi.KnowledgeObject{
			ID: id, Type: typ, Subtype: subtype, Status: "active",
			Embeddings: emb,
			CreatedAt:  time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("vsf-note", "note", "daily", []float32{1, 0, 0, 0})
	mk("vsf-article", "article", "", []float32{1, 0, 0, 0})
	mk("vsf-note-2", "note", "weekly", []float32{0, 1, 0, 0})

	// Two filter params + the bound vector: placeholders must not collide.
	results, err := drv.Objects().VectorSearch(ctx, []float32{1, 0, 0, 0},
		storage.ObjectFilter{Type: "note", Subtype: "daily", Limit: 10})
	if err != nil {
		t.Fatalf("filtered VectorSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != "vsf-note" {
		ids := make([]string, len(results))
		for i, r := range results {
			ids[i] = r.ID
		}
		t.Fatalf("filtered VectorSearch: got %v, want [vsf-note]", ids)
	}
	if s, ok := results[0].Metadata["score"].(float64); !ok || s < 0.99 {
		t.Errorf("score for exact match: got %v, want ~1.0", results[0].Metadata["score"])
	}
}
