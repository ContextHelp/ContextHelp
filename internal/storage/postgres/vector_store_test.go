//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// freshVectorDriver provisions a fresh database migrated at the given vector
// dimension (small dimensions keep tests fast; the HNSW index still builds).
func freshVectorDriver(t *testing.T, dim int) *pgdrv.Driver {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	drv.SetVectorDimension(dim)
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return drv
}

// createBareObject inserts an object without embeddings so VectorStore.Upsert
// has a row to attach to.
func createBareObject(t *testing.T, drv *pgdrv.Driver, id string) {
	t.Helper()
	if err := drv.Objects().Create(context.Background(), makePgFTSObject(id, "note", "vector store fixture "+id)); err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
}

func TestPgVectorStore_Singleton(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	if drv.Vectors() != drv.Vectors() {
		t.Error("Vectors() allocates a fresh store per call; want one shared instance")
	}
}

func TestPgVectorStore_UpsertSearchRoundtrip(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	vs := drv.Vectors()

	createBareObject(t, drv, "vec-1")
	createBareObject(t, drv, "vec-2")

	if err := vs.Upsert(ctx, "vec-1", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("upsert vec-1: %v", err)
	}
	if err := vs.Upsert(ctx, "vec-2", []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("upsert vec-2: %v", err)
	}

	hits, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits: got %d want 2", len(hits))
	}
	if hits[0].ID != "vec-1" {
		t.Errorf("nearest: got %q want vec-1", hits[0].ID)
	}
	// Score is the raw cosine distance, ascending — the SQLite VecStore
	// contract.
	if hits[0].Score > 1e-6 {
		t.Errorf("exact-match distance: got %v want ~0", hits[0].Score)
	}
	if hits[1].Score < hits[0].Score {
		t.Errorf("distances not ascending: %v then %v", hits[0].Score, hits[1].Score)
	}
}

// TestPgVectorStore_UpsertMissingObject pins the deliberate divergence from
// SQLite's standalone vec0 table: on Postgres the vector index is the
// objects.embedding column, so a vector cannot exist without its object.
func TestPgVectorStore_UpsertMissingObject(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	err := drv.Vectors().Upsert(context.Background(), "ghost", []float32{1, 0, 0, 0})
	if err == nil {
		t.Fatal("upsert for missing object succeeded; want error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should name the object: %v", err)
	}
}

func TestPgVectorStore_UpsertOverwrite(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	vs := drv.Vectors()

	createBareObject(t, drv, "vec-ow")
	if err := vs.Upsert(ctx, "vec-ow", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if err := vs.Upsert(ctx, "vec-ow", []float32{0, 0, 0, 1}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	hits, err := vs.Search(ctx, []float32{0, 0, 0, 1}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Score > 1e-6 {
		t.Fatalf("overwritten vector not found at distance 0: %+v", hits)
	}
}

func TestPgVectorStore_DeleteAndMissingDelete(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	vs := drv.Vectors()

	createBareObject(t, drv, "vec-del")
	if err := vs.Upsert(ctx, "vec-del", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := vs.Delete(ctx, "vec-del"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	hits, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("hits after delete: got %d want 0", len(hits))
	}

	// Deleting an unknown id is a no-op, mirroring SQLite.
	if err := vs.Delete(ctx, "never-existed"); err != nil {
		t.Errorf("delete unknown id: got %v want nil", err)
	}
}

// TestPgVectorStore_ZeroVectorSemantics pins the cosine contract edges:
// upserting a zero-magnitude vector clears the index entry (cosine is
// undefined for it), and a zero query vector matches nothing.
func TestPgVectorStore_ZeroVectorSemantics(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	vs := drv.Vectors()

	createBareObject(t, drv, "vec-zero")
	if err := vs.Upsert(ctx, "vec-zero", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("upsert real: %v", err)
	}
	if err := vs.Upsert(ctx, "vec-zero", []float32{0, 0, 0, 0}); err != nil {
		t.Fatalf("upsert zero: %v", err)
	}
	hits, err := vs.Search(ctx, []float32{1, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("zero-upserted vector still indexed: %+v", hits)
	}

	createBareObject(t, drv, "vec-real")
	if err := vs.Upsert(ctx, "vec-real", []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("upsert vec-real: %v", err)
	}
	hits, err = vs.Search(ctx, []float32{0, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("zero-query search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("zero query returned hits: %+v", hits)
	}
}

func TestPgVectorStore_Count(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	vs := drv.Vectors()

	n, err := vs.Count(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("initial count: got %d want 0", n)
	}

	createBareObject(t, drv, "vec-c1")
	createBareObject(t, drv, "vec-c2")
	if err := vs.Upsert(ctx, "vec-c1", []float32{1, 0, 0, 0}); err != nil {
		t.Fatalf("upsert c1: %v", err)
	}
	if err := vs.Upsert(ctx, "vec-c2", []float32{0, 1, 0, 0}); err != nil {
		t.Fatalf("upsert c2: %v", err)
	}
	n, err = vs.Count(ctx)
	if err != nil {
		t.Fatalf("count after upserts: %v", err)
	}
	if n != 2 {
		t.Errorf("count: got %d want 2", n)
	}
}

// TestPostgresVectorSearch_SkipsUnrankableRows pins that rows holding a
// zero-magnitude embedding (undefined cosine distance) never surface from
// either search surface — matching the SQLite ANN leg, which refuses to
// index them.
func TestPostgresVectorSearch_SkipsUnrankableRows(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	zero := makePgFTSObject("vec-nan", "note", "zero embedding fixture")
	zero.Embeddings = []float32{0, 0, 0, 0}
	if err := drv.Objects().Create(ctx, zero); err != nil {
		t.Fatalf("create zero-embedding object: %v", err)
	}
	real := makePgFTSObject("vec-ok", "note", "real embedding fixture")
	real.Embeddings = []float32{1, 0, 0, 0}
	if err := drv.Objects().Create(ctx, real); err != nil {
		t.Fatalf("create real object: %v", err)
	}

	results, err := drv.Objects().VectorSearch(ctx, []float32{1, 0, 0, 0}, storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != "vec-ok" {
		t.Fatalf("VectorSearch results: got %+v want single vec-ok", ids(results))
	}
	score := results[0].Metadata["score"].(float64)
	if score < -1.000001 || score > 1.000001 {
		t.Errorf("score %v outside [-1, 1]", score)
	}

	hits, err := drv.Vectors().Search(ctx, []float32{1, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("VectorStore.Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "vec-ok" {
		t.Fatalf("VectorStore hits: got %+v want single vec-ok", hits)
	}
}

// TestPostgresVectorSearch_FilteredRecall pins the filtered-KNN recall
// contract: a selective filter plus limit must return all qualifying
// neighbors even when many better-ranked rows fail the filter. pgvector
// applies WHERE after index traversal, so this is exactly the under-return
// case iterative index scans (>= 0.8.0) exist to fix.
func TestPostgresVectorSearch_FilteredRecall(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	query := []float32{1, 0, 0, 0}
	// 60 near-query objects of the majority type...
	for i := 0; i < 60; i++ {
		obj := makePgFTSObject(fmt.Sprintf("hay-%02d", i), "hay", "haystack filler")
		obj.Embeddings = []float32{1, float32(i) * 0.001, 0, 0}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create hay-%02d: %v", i, err)
		}
	}
	// ...and 5 far-from-query objects of the selective type.
	for i := 0; i < 5; i++ {
		obj := makePgFTSObject(fmt.Sprintf("needle-%d", i), "needle", "needle fixture")
		obj.Embeddings = []float32{0, 0, 1, float32(i) * 0.01}
		if err := drv.Objects().Create(ctx, obj); err != nil {
			t.Fatalf("create needle-%d: %v", i, err)
		}
	}

	results, err := drv.Objects().VectorSearch(ctx, query, storage.ObjectFilter{Type: "needle", Limit: 5})
	if err != nil {
		t.Fatalf("filtered VectorSearch: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("filtered recall: got %d results want all 5 qualifying neighbors (%v)", len(results), ids(results))
	}
	for _, r := range results {
		if !strings.HasPrefix(r.ID, "needle-") {
			t.Errorf("non-qualifying hit %q leaked through type filter", r.ID)
		}
	}
}

// TestPostgresVectorSearchNodeAware pins node-type ALL-of filtering and the
// ReturnNodeHits DocumentView projection on the vector leg, mirroring SQLite.
func TestPostgresVectorSearchNodeAware(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	withDecision := makePgFTSObject("vec-na1", "note", "decision-bearing vector fixture")
	withDecision.Embeddings = []float32{1, 0, 0, 0}
	withDecision.Graph.Nodes = append(withDecision.Graph.Nodes, pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("vec-na1", pluginapi.NodeTypeDecision, 1),
		NodeType: pluginapi.NodeTypeDecision,
		Label:    "Decision",
		Content:  "adopt pgvector",
		Order:    1,
	})
	if err := drv.Objects().Create(ctx, withDecision); err != nil {
		t.Fatalf("create withDecision: %v", err)
	}
	summaryOnly := makePgFTSObject("vec-na2", "note", "summary-only vector fixture")
	summaryOnly.Embeddings = []float32{0.9, 0.1, 0, 0}
	if err := drv.Objects().Create(ctx, summaryOnly); err != nil {
		t.Fatalf("create summaryOnly: %v", err)
	}

	all, err := drv.Objects().VectorSearchNodeAware(ctx, []float32{1, 0, 0, 0},
		storage.ObjectFilter{}, pluginapi.NodeAwareFilter{})
	if err != nil {
		t.Fatalf("VectorSearchNodeAware (no filter): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered results: got %d want 2", len(all))
	}

	filtered, err := drv.Objects().VectorSearchNodeAware(ctx, []float32{1, 0, 0, 0},
		storage.ObjectFilter{},
		pluginapi.NodeAwareFilter{NodeTypes: []string{pluginapi.NodeTypeDecision}, ReturnNodeHits: true})
	if err != nil {
		t.Fatalf("VectorSearchNodeAware (decision filter): %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered results: got %d want 1", len(filtered))
	}
	if filtered[0].Object.ID != "vec-na1" {
		t.Errorf("filtered hit: got %q want vec-na1", filtered[0].Object.ID)
	}
	if filtered[0].DocumentView == nil {
		t.Error("DocumentView nil with ReturnNodeHits=true")
	}
}
