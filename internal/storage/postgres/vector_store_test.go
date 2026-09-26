//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
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

// indexModel registers modelID at dim and builds its per-model index. Until
// the driver's EmbeddingStore is implemented (errors.ErrUnsupported) the
// index is created by hand from the ADR-071 amendment's DDL, so the query
// path's join, literal predicate and cast are pinned against that shape.
func indexModel(t *testing.T, drv *pgdrv.Driver, modelID string, dim int) {
	t.Helper()
	ctx := context.Background()
	m := registry.Model{ModelID: modelID, Provider: "fixture", Dimension: dim, ConfigJSON: "{}"}
	if err := registry.NewFor(drv.DB(), "postgres").Register(ctx, m, false); err != nil {
		t.Fatalf("register %s: %v", modelID, err)
	}
	err := drv.Embeddings().EnsureIndex(ctx, storage.EmbeddingModelSpec{ModelID: modelID, Provider: m.Provider, Dimension: dim})
	if !errors.Is(err, errors.ErrUnsupported) {
		if err != nil {
			t.Fatalf("EnsureIndex %s: %v", modelID, err)
		}
		return
	}
	ddl := fmt.Sprintf(`CREATE INDEX idx_%s ON embeddings USING hnsw ((vector::vector(%d)) vector_cosine_ops) WHERE model_id = '%s'`,
		storage.EmbeddingIndexName(modelID), dim, modelID)
	if _, err := drv.DB().ExecContext(ctx, ddl); err != nil {
		t.Fatalf("hand-built index for %s: %v", modelID, err)
	}
}

// putVectors stores an object's chunks under modelID, by EmbeddingStore.Put
// once implemented and by hand until then (skipping zero-magnitude vectors,
// as Put does).
func putVectors(t *testing.T, drv *pgdrv.Driver, objectID, modelID string, chunks ...[]float32) {
	t.Helper()
	ctx := context.Background()
	vs := make([]storage.ObjectVector, len(chunks))
	for i, c := range chunks {
		vs[i] = storage.ObjectVector{ModelID: modelID, ChunkIdx: i, Vector: c}
	}
	err := drv.Embeddings().Put(ctx, objectID, vs)
	if !errors.Is(err, errors.ErrUnsupported) {
		if err != nil {
			t.Fatalf("Put %s: %v", objectID, err)
		}
		return
	}
	for _, v := range vs {
		if magnitude(v.Vector) == 0 {
			continue
		}
		if _, err := drv.DB().ExecContext(ctx,
			`INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at) VALUES ($1, $2, $3, $4::vector, now())`,
			objectID, modelID, v.ChunkIdx, pgVector(v.Vector)); err != nil {
			t.Fatalf("insert %s/%s/%d: %v", objectID, modelID, v.ChunkIdx, err)
		}
	}
}

func magnitude(v []float32) float64 {
	var s float64
	for _, f := range v {
		s += float64(f) * float64(f)
	}
	return math.Sqrt(s)
}

func pgVector(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = fmt.Sprintf("%g", f)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// vectorObject creates an object with a projected body and stores its
// vector chunks under modelID.
func vectorObject(t *testing.T, drv *pgdrv.Driver, obj *storage.KnowledgeObject, modelID string, chunks ...[]float32) {
	t.Helper()
	if err := drv.Objects().Create(context.Background(), obj); err != nil {
		t.Fatalf("create %s: %v", obj.ID, err)
	}
	putVectors(t, drv, obj.ID, modelID, chunks...)
}

func pgQuery(modelID string, vec ...float32) storage.VectorQuery {
	return storage.VectorQuery{ModelID: modelID, Vector: vec}
}

// VectorSearch ranks through the queried model's own index at that model's
// dimension: cosine order, score = 1 - distance, one hit per object (its
// closest chunk), and nothing from another model's rows.
func TestPostgresVectorSearch_PerModelRankAndScore(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()
	indexModel(t, drv, "pg-a", 4)
	indexModel(t, drv, "pg-b", 3)

	vectorObject(t, drv, makePgFTSObject("pm-top", "note", "top"), "pg-a", []float32{0, 1, 0, 0}, []float32{2, 0, 0, 0})
	vectorObject(t, drv, makePgFTSObject("pm-mid", "note", "mid"), "pg-a", []float32{0.5, 0.8660254, 0, 0})
	vectorObject(t, drv, makePgFTSObject("pm-zero", "note", "zero"), "pg-a", []float32{0, 0, 0, 0})
	vectorObject(t, drv, makePgFTSObject("pm-b-only", "note", "b only"), "pg-b", []float32{1, 0, 0})

	results, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-a", 1, 0, 0, 0), storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("VectorSearch: %v", err)
	}
	if got := ids(results); len(got) != 2 || got[0] != "pm-top" || got[1] != "pm-mid" {
		t.Fatalf("results = %v, want [pm-top pm-mid] (chunks collapsed, zero vector and pg-b rows absent)", got)
	}
	for i, want := range []float64{1.0, 0.5} {
		if s := results[i].Metadata["score"].(float64); math.Abs(s-want) > 1e-4 {
			t.Errorf("%s score = %v, want %v (1 - cosine distance of the closest chunk)", results[i].ID, s, want)
		}
	}

	results, err = drv.Objects().VectorSearch(ctx, pgQuery("pg-b", 1, 0, 0), storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("VectorSearch pg-b: %v", err)
	}
	if got := ids(results); len(got) != 1 || got[0] != "pm-b-only" {
		t.Fatalf("pg-b results = %v, want [pm-b-only]", got)
	}
}

func TestPostgresVectorSearch_IndexMissingAndDimension(t *testing.T) {
	drv := freshVectorDriver(t, 4)
	ctx := context.Background()

	if _, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-unregistered", 1, 0, 0, 0), storage.ObjectFilter{}); !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		t.Fatalf("unregistered model: err = %v, want ErrEmbeddingIndexMissing", err)
	}
	m := registry.Model{ModelID: "pg-noindex", Provider: "fixture", Dimension: 4, ConfigJSON: "{}"}
	if err := registry.NewFor(drv.DB(), "postgres").Register(ctx, m, false); err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-noindex", 1, 0, 0, 0), storage.ObjectFilter{}); !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		t.Fatalf("registered without an index: err = %v, want ErrEmbeddingIndexMissing", err)
	}
	indexModel(t, drv, "pg-dim", 4)
	if _, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-dim", 1, 0, 0), storage.ObjectFilter{}); !errors.Is(err, storage.ErrEmbeddingDimension) {
		t.Fatalf("3-dim query on a 4-dim index: err = %v, want ErrEmbeddingDimension", err)
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
	indexModel(t, drv, "pg-recall", 4)

	// 60 near-query objects of the majority type...
	for i := 0; i < 60; i++ {
		vectorObject(t, drv, makePgFTSObject(fmt.Sprintf("hay-%02d", i), "hay", "haystack filler"),
			"pg-recall", []float32{1, float32(i) * 0.001, 0, 0})
	}
	// ...and 5 far-from-query objects of the selective type.
	for i := 0; i < 5; i++ {
		vectorObject(t, drv, makePgFTSObject(fmt.Sprintf("needle-%d", i), "needle", "needle fixture"),
			"pg-recall", []float32{0, 0, 1, float32(i) * 0.01})
	}

	results, err := drv.Objects().VectorSearch(ctx, pgQuery("pg-recall", 1, 0, 0, 0), storage.ObjectFilter{Type: "needle", Limit: 5})
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
	indexModel(t, drv, "pg-na", 4)

	withDecision := makePgFTSObject("vec-na1", "note", "decision-bearing vector fixture")
	withDecision.Graph.Nodes = append(withDecision.Graph.Nodes, pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("vec-na1", pluginapi.NodeTypeDecision, 1),
		NodeType: pluginapi.NodeTypeDecision,
		Label:    "Decision",
		Content:  "adopt pgvector",
		Order:    1,
	})
	vectorObject(t, drv, withDecision, "pg-na", []float32{1, 0, 0, 0})
	vectorObject(t, drv, makePgFTSObject("vec-na2", "note", "summary-only vector fixture"), "pg-na", []float32{0.9, 0.1, 0, 0})

	all, err := drv.Objects().VectorSearchNodeAware(ctx, pgQuery("pg-na", 1, 0, 0, 0),
		storage.ObjectFilter{}, pluginapi.NodeAwareFilter{})
	if err != nil {
		t.Fatalf("VectorSearchNodeAware (no filter): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered results: got %d want 2", len(all))
	}

	filtered, err := drv.Objects().VectorSearchNodeAware(ctx, pgQuery("pg-na", 1, 0, 0, 0),
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
