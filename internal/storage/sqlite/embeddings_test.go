package sqlite

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// embFixture is a driver with one registered, indexed model.
type embFixture struct {
	d     *Driver
	store *EmbeddingStore
	spec  storage.EmbeddingModelSpec
	ix    vecIndex
}

func newEmbFixture(t *testing.T, modelID string, dim int) *embFixture {
	t.Helper()
	d := newTestDriver(t)
	f := &embFixture{d: d, store: d.Embeddings().(*EmbeddingStore)}
	f.spec = f.registerModel(t, modelID, dim)
	f.ix = vecIndexFor(modelID)
	return f
}

func (f *embFixture) registerModel(t *testing.T, modelID string, dim int) storage.EmbeddingModelSpec {
	t.Helper()
	insertModelRow(t, f.d, modelID, "test", dim)
	spec := storage.EmbeddingModelSpec{ModelID: modelID, Provider: "test", Dimension: dim}
	if err := f.store.EnsureIndex(context.Background(), spec); err != nil {
		t.Fatalf("EnsureIndex(%s): %v", modelID, err)
	}
	return spec
}

func insertModelRow(t *testing.T, d *Driver, modelID, provider string, dim int) {
	t.Helper()
	if _, err := d.db.Exec(`
		INSERT INTO embedding_models (model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES (?, ?, ?, 0, ?, '{}')`,
		modelID, provider, dim, time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("insert model %s: %v", modelID, err)
	}
}

func insertObjectRow(t *testing.T, d *Driver, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.Exec(
		`INSERT INTO objects (id, type, created_at, updated_at) VALUES (?, 'note', ?, ?)`,
		id, now, now,
	); err != nil {
		t.Fatalf("insert object %s: %v", id, err)
	}
}

func vecBlob(t *testing.T, v ...float32) []byte {
	t.Helper()
	b, err := sqlite_vec.SerializeFloat32(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// vecRowIDs lists the rowids currently in the model's vec0 table.
func (f *embFixture) vecRowIDs(t *testing.T) []int64 {
	t.Helper()
	return queryIDs(t, f.d, `SELECT rowid FROM `+f.ix.table+` ORDER BY rowid`)
}

func (f *embFixture) canonicalIDs(t *testing.T) []int64 {
	t.Helper()
	return queryIDs(t, f.d, `SELECT id FROM embeddings WHERE model_id = ? ORDER BY id`, f.spec.ModelID)
}

func queryIDs(t *testing.T, d *Driver, query string, args ...any) []int64 {
	t.Helper()
	rows, err := d.db.Query(query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (f *embFixture) search(t *testing.T, q ...float32) []storage.EmbeddingHit {
	t.Helper()
	hits, err := f.store.Search(context.Background(), storage.VectorQuery{ModelID: f.spec.ModelID, Vector: q, TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	return hits
}

// TestEmbeddingIndex_DDLNamesAndLiteral pins the per-model schema objects:
// hash-derived vec0 table and trigger names, the cosine metric at the
// registry dimension, and the model_id literal in each trigger's WHEN.
func TestEmbeddingIndex_DDLNamesAndLiteral(t *testing.T) {
	f := newEmbFixture(t, "ollama-bge-m3@2026-09-26", 8)
	if f.ix.table != "vec_"+storage.EmbeddingIndexName(f.spec.ModelID) {
		t.Fatalf("table name %q", f.ix.table)
	}
	var ddl string
	if err := f.d.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = ?`, f.ix.table).Scan(&ddl); err != nil {
		t.Fatalf("vec0 table missing: %v", err)
	}
	if !strings.Contains(ddl, "float[8]") || !strings.Contains(ddl, "distance_metric=cosine") {
		t.Errorf("vec0 DDL = %q", ddl)
	}
	for _, trig := range []string{f.ix.insTrig, f.ix.updTrig, f.ix.delTrig} {
		var sql string
		if err := f.d.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trig).Scan(&sql); err != nil {
			t.Fatalf("trigger %s missing: %v", trig, err)
		}
		if !strings.Contains(sql, "model_id = 'ollama-bge-m3@2026-09-26'") {
			t.Errorf("trigger %s lacks the model_id literal: %s", trig, sql)
		}
	}
	row, err := LoadIndexSignature(context.Background(), f.d.db, EmbeddingSignatureID(f.spec.ModelID))
	if err != nil || row == nil {
		t.Fatalf("signature: %v %v", row, err)
	}
	wantHash, _ := indexsig.ComputeEmbedding(f.spec.ModelID, "test", 8, "vec0", "cosine", ddl)
	if row.SignatureHash != wantHash {
		t.Errorf("signature hash = %s, want ComputeEmbedding over the live DDL %s", row.SignatureHash, wantHash)
	}
}

// TestEmbeddingIndex_TriggersSyncDirectWrites pins that the vec0 table
// follows canonical rows written outside Put: insert, vector update,
// delete, and ON DELETE CASCADE from objects. Other models' rows never
// reach this model's index.
func TestEmbeddingIndex_TriggersSyncDirectWrites(t *testing.T) {
	f := newEmbFixture(t, "trig@1", 4)
	other := f.registerModel(t, "trig-other@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "t-1")
	insertObjectRow(t, f.d, "t-2")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, r := range []struct {
		obj, model string
		v          []float32
	}{
		{"t-1", f.spec.ModelID, []float32{1, 0, 0, 0}},
		{"t-2", f.spec.ModelID, []float32{0, 1, 0, 0}},
		{"t-1", other.ModelID, []float32{0, 0, 1, 0}},
	} {
		if _, err := f.d.db.ExecContext(ctx, `
			INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at)
			VALUES (?, ?, 0, ?, ?)`, r.obj, r.model, vecBlob(t, r.v...), now); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if got, want := f.vecRowIDs(t), f.canonicalIDs(t); len(got) != 2 || !equalIDs(got, want) {
		t.Fatalf("after insert: vec rowids %v, canonical ids %v", got, want)
	}

	if _, err := f.d.db.ExecContext(ctx, `UPDATE embeddings SET vector = ? WHERE object_id = 't-2' AND model_id = ?`,
		vecBlob(t, 1, 0, 0, 0), f.spec.ModelID); err != nil {
		t.Fatalf("update: %v", err)
	}
	hits := f.search(t, 1, 0, 0, 0)
	if len(hits) != 2 || hits[1].Distance > 1e-5 {
		t.Errorf("update not reflected in index: %+v", hits)
	}

	if _, err := f.d.db.ExecContext(ctx, `DELETE FROM objects WHERE id = 't-1'`); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if got, want := f.vecRowIDs(t), f.canonicalIDs(t); len(got) != 1 || !equalIDs(got, want) {
		t.Errorf("after cascade: vec rowids %v, canonical ids %v", got, want)
	}
	if _, err := f.d.db.ExecContext(ctx, `DELETE FROM embeddings WHERE model_id = ?`, f.spec.ModelID); err != nil {
		t.Fatalf("delete rows: %v", err)
	}
	if got := f.vecRowIDs(t); len(got) != 0 {
		t.Errorf("after delete: vec rowids %v", got)
	}
}

// TestEmbeddingIndex_WrongDimensionDirectInsertAborts pins the vec0
// backstop: a canonical row of the wrong dimension aborts its statement, so
// neither the row nor an index entry is written.
func TestEmbeddingIndex_WrongDimensionDirectInsertAborts(t *testing.T) {
	f := newEmbFixture(t, "abort@1", 4)
	insertObjectRow(t, f.d, "a-1")
	_, err := f.d.db.Exec(`
		INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at)
		VALUES ('a-1', ?, 0, ?, 'now')`, f.spec.ModelID, vecBlob(t, 1, 0, 0))
	if err == nil {
		t.Fatal("wrong-dimension row accepted")
	}
	if ids := f.canonicalIDs(t); len(ids) != 0 {
		t.Errorf("canonical row written despite abort: %v", ids)
	}
}

// TestEmbeddingIndex_RebuildRefillsFromCanonicalRows pins that a drifted
// signature rebuilds the index from canonical rows, not just re-stamps it.
func TestEmbeddingIndex_RebuildRefillsFromCanonicalRows(t *testing.T) {
	f := newEmbFixture(t, "refill@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "r-1")
	insertObjectRow(t, f.d, "r-2")
	if err := f.store.Put(ctx, "r-1", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Put(ctx, "r-2", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{0, 1, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	// Corrupt the derived index and the stamp.
	if _, err := f.d.db.Exec(`DELETE FROM ` + f.ix.table); err != nil {
		t.Fatal(err)
	}
	if err := UpsertIndexSignature(ctx, f.d.db, EmbeddingSignatureID(f.spec.ModelID), "stale", ""); err != nil {
		t.Fatal(err)
	}
	if err := f.store.EnsureIndex(ctx, f.spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if got, want := f.vecRowIDs(t), f.canonicalIDs(t); len(got) != 2 || !equalIDs(got, want) {
		t.Errorf("rebuild did not refill: vec rowids %v, canonical %v", got, want)
	}
}

// TestEmbeddingIndex_LiveDriftRebuilds covers drift the stored stamp cannot
// see: a vec0 table recreated with another DDL, or a missing trigger, while
// the stamp still matches. EnsureIndex must converge the live objects.
func TestEmbeddingIndex_LiveDriftRebuilds(t *testing.T) {
	f := newEmbFixture(t, "live@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "l-1")
	if err := f.store.Put(ctx, "l-1", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}

	// L2 table under the same name, stamp untouched.
	tx, err := f.d.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := dropVecIndex(ctx, tx, f.ix); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`CREATE VIRTUAL TABLE ` + f.ix.table + ` USING vec0(embedding float[4])`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := f.store.EnsureIndex(ctx, f.spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	live, err := indexsig.SQLiteVectorIndexFor(ctx, f.d.db, f.ix.table)
	if err != nil || live.OpsClass != "cosine" {
		t.Fatalf("live index after rebuild = %+v (%v), want cosine", live, err)
	}
	if ok, _ := vecIndexComplete(ctx, f.d.db, f.ix); !ok {
		t.Fatal("triggers missing after rebuild")
	}
	if got := f.vecRowIDs(t); len(got) != 1 {
		t.Errorf("rebuild did not refill: %v", got)
	}

	// Missing trigger only.
	if _, err := f.d.db.Exec(`DROP TRIGGER ` + f.ix.insTrig); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Put(ctx, "l-1", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{0, 1, 0, 0}}}); !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
		t.Errorf("Put with a missing trigger = %v, want ErrEmbeddingIndexMissing", err)
	}
	if err := f.store.EnsureIndex(ctx, f.spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if ok, _ := vecIndexComplete(ctx, f.d.db, f.ix); !ok {
		t.Error("missing trigger not recreated")
	}
}

// TestEmbeddingIndex_MismatchedCanonicalRowsBlockRebuild pins that a
// rebuild never silently drops canonical rows whose length disagrees with
// the registry dimension: EnsureIndex fails with ErrEmbeddingDimension and
// the transaction leaves the previous index in place.
func TestEmbeddingIndex_MismatchedCanonicalRowsBlockRebuild(t *testing.T) {
	f := newEmbFixture(t, "mismatch@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "m-1")
	if err := f.store.Put(ctx, "m-1", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	before := f.vecRowIDs(t)

	grown := f.spec
	grown.Dimension = 8
	err := f.store.EnsureIndex(ctx, grown)
	if !errors.Is(err, storage.ErrEmbeddingDimension) {
		t.Fatalf("EnsureIndex over mismatched rows = %v, want ErrEmbeddingDimension", err)
	}
	var ddl string
	if err := f.d.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = ?`, f.ix.table).Scan(&ddl); err != nil {
		t.Fatalf("index dropped by a failed rebuild: %v", err)
	}
	if !strings.Contains(ddl, "float[4]") {
		t.Errorf("failed rebuild changed the index: %s", ddl)
	}
	if got := f.vecRowIDs(t); !equalIDs(got, before) {
		t.Errorf("failed rebuild changed entries: %v -> %v", before, got)
	}
	if row, _ := LoadIndexSignature(ctx, f.d.db, EmbeddingSignatureID(f.spec.ModelID)); row == nil {
		t.Error("failed rebuild removed the signature")
	}
}

// TestEmbeddingStore_PutRejectsNonFiniteAndNegativeChunk pins input checks
// the index cannot rank: NaN/Inf components and negative chunk indexes.
func TestEmbeddingStore_PutRejectsNonFiniteAndNegativeChunk(t *testing.T) {
	f := newEmbFixture(t, "finite@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "f-1")
	nan := float32(math.NaN())
	for name, v := range map[string]storage.ObjectVector{
		"nan":      {ModelID: f.spec.ModelID, Vector: []float32{nan, 0, 0, 0}},
		"inf":      {ModelID: f.spec.ModelID, Vector: []float32{float32(math.Inf(1)), 0, 0, 0}},
		"negative": {ModelID: f.spec.ModelID, ChunkIdx: -1, Vector: []float32{1, 0, 0, 0}},
	} {
		if err := f.store.Put(ctx, "f-1", []storage.ObjectVector{v}); err == nil {
			t.Errorf("%s: Put accepted", name)
		}
	}
	if ids := f.canonicalIDs(t); len(ids) != 0 {
		t.Errorf("rejected Puts wrote rows: %v", ids)
	}
}

// TestEmbeddingStore_SearchKCeiling pins that a TopK above vec0's k limit
// clamps instead of erroring.
func TestEmbeddingStore_SearchKCeiling(t *testing.T) {
	f := newEmbFixture(t, "kmax@1", 4)
	ctx := context.Background()
	insertObjectRow(t, f.d, "k-1")
	if err := f.store.Put(ctx, "k-1", []storage.ObjectVector{{ModelID: f.spec.ModelID, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	hits, err := f.store.Search(ctx, storage.VectorQuery{ModelID: f.spec.ModelID, Vector: []float32{1, 0, 0, 0}, TopK: vec0KMax + 1000})
	if err != nil || len(hits) != 1 {
		t.Fatalf("Search(TopK above k max) = %+v, %v", hits, err)
	}
}

// TestEmbeddingIndex_BuiltOnOpen pins the post-migrate pass: a model
// registered without an index gets one on the next open, and a model whose
// ID or dimension cannot be indexed is skipped without failing the open.
func TestEmbeddingIndex_BuiltOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "open.db")
	ctx := context.Background()
	d1, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d1.Init(ctx); err != nil {
		t.Fatal(err)
	}
	insertModelRow(t, d1, "late@1", "test", 4)
	insertModelRow(t, d1, "bad id", "test", 4)
	insertModelRow(t, d1, "huge@1", "test", storage.SQLiteVecMaxDimension+1)
	insertModelRow(t, d1, "unprobed@1", "test", 0)
	if err := d1.Close(ctx); err != nil {
		t.Fatal(err)
	}

	d2, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d2.Close(ctx) })
	if err := d2.Init(ctx); err != nil {
		t.Fatalf("open with unindexable models: %v", err)
	}
	if ok, err := vecIndexComplete(ctx, d2.db, vecIndexFor("late@1")); err != nil || !ok {
		t.Errorf("index for late@1 not built on open: %v %v", ok, err)
	}
	for _, id := range []string{"bad id", "huge@1", "unprobed@1"} {
		if ok, _ := vecIndexComplete(ctx, d2.db, vecIndexFor(id)); ok {
			t.Errorf("index built for unindexable model %q", id)
		}
	}
	// PurgeModel still cleans up a model EnsureIndex refuses.
	if err := d2.Embeddings().PurgeModel(ctx, "bad id"); err != nil {
		t.Errorf("PurgeModel(invalid id): %v", err)
	}
}
