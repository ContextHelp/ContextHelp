//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// TestPostgres_VectorSearch_FilterWithBoundVector guards the placeholder
// layout of VectorSearch: the query vector is bound as a parameter and the
// filter conditions number themselves around it. A mis-numbered placeholder
// surfaces here as a driver error or a wrong result set.
func TestPostgres_VectorSearch_FilterWithBoundVector(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	indexModel(t, drv, "pg-bound", 4)

	mk := func(id, typ, subtype string, emb []float32) {
		t.Helper()
		obj := &pluginapi.KnowledgeObject{
			ID: id, Type: typ, Subtype: subtype, Status: "active",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		vectorObject(t, drv, obj, "pg-bound", emb)
	}
	mk("vsf-note", "note", "daily", []float32{1, 0, 0, 0})
	mk("vsf-article", "article", "", []float32{1, 0, 0, 0})
	mk("vsf-note-2", "note", "weekly", []float32{0, 1, 0, 0})

	// Two filter params + the bound vector: placeholders must not collide.
	results, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-bound", 1, 0, 0, 0),
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
