//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_EmbeddingStore runs the cross-driver EmbeddingStore
// contract against a fresh Postgres database.
func TestConformance_EmbeddingStore(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.EmbeddingStoreConformance(t, drv)
}

func pgInsertModel(t *testing.T, drv *pgdrv.Driver, modelID, provider string, dim int) {
	t.Helper()
	if _, err := drv.DB().Exec(`
		INSERT INTO embedding_models (model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES ($1, $2, $3, 0, NOW(), '{}'::jsonb)`, modelID, provider, dim); err != nil {
		t.Fatalf("insert model %s: %v", modelID, err)
	}
}

func pgInsertObject(t *testing.T, drv *pgdrv.Driver, id string) {
	t.Helper()
	if _, err := drv.DB().Exec(
		`INSERT INTO objects (id, type, created_at, updated_at) VALUES ($1, 'note', NOW(), NOW())`, id,
	); err != nil {
		t.Fatalf("insert object %s: %v", id, err)
	}
}

func pgScalar(t *testing.T, drv *pgdrv.Driver, query string, args ...any) int {
	t.Helper()
	var n int
	if err := drv.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func pgEnsure(t *testing.T, drv *pgdrv.Driver, modelID string, dim int) storage.EmbeddingModelSpec {
	t.Helper()
	pgInsertModel(t, drv, modelID, "test", dim)
	spec := storage.EmbeddingModelSpec{ModelID: modelID, Provider: "test", Dimension: dim}
	if err := drv.Embeddings().EnsureIndex(context.Background(), spec); err != nil {
		t.Fatalf("EnsureIndex(%s): %v", modelID, err)
	}
	return spec
}

// TestPgEmbeddingIndex_DDLCheckAndSignature pins the live per-model
// objects: a partial HNSW cosine index over the dimension cast, the
// per-model dimension CHECK, and a stamp hashed from the desired shape.
func TestPgEmbeddingIndex_DDLCheckAndSignature(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	spec := pgEnsure(t, drv, "ollama-bge-m3@2026-09-26", 8)
	index, check := pgdrv.EmbeddingIndexNamesForTest(spec.ModelID)

	var def string
	if err := drv.DB().QueryRow(`SELECT indexdef FROM pg_indexes WHERE indexname = $1`, index).Scan(&def); err != nil {
		t.Fatalf("index %s missing: %v", index, err)
	}
	for _, part := range []string{"USING hnsw", "::vector(8)", "vector_cosine_ops", "model_id = 'ollama-bge-m3@2026-09-26'"} {
		if !strings.Contains(def, part) {
			t.Errorf("indexdef missing %q: %s", part, def)
		}
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_constraint WHERE conname = $1`, check); n != 1 {
		t.Errorf("dimension CHECK %s missing", check)
	}
	row, err := indexsig.Load(ctx, drv.DB(), indexsig.DialectPostgres, indexsig.EmbeddingSignatureID(spec.ModelID))
	if err != nil || row == nil {
		t.Fatalf("signature: %+v %v", row, err)
	}
	want, _ := indexsig.ComputeEmbedding(spec.ModelID, "test", 8, "hnsw", "vector_cosine_ops", "")
	if row.SignatureHash != want {
		t.Errorf("signature %s, want %s", row.SignatureHash, want)
	}
	live, err := indexsig.PostgresVectorIndexFor(ctx, drv.DB(), index)
	if err != nil || live != (indexsig.VectorIndexDescription{Method: "hnsw", OpsClass: "vector_cosine_ops"}) {
		t.Errorf("live description %+v, %v", live, err)
	}
	if absent, _ := indexsig.PostgresVectorIndexFor(ctx, drv.DB(), "idx_emb_nope"); absent.Exists() {
		t.Errorf("absent index described as %+v", absent)
	}
}

// TestPgEmbeddingSearch_UsesPartialIndexUnderGenericPlan pins the literal
// predicate: even when Postgres plans generically (a cached prepared
// statement), the KNN query Search runs is served by the model's partial
// index. A bound model_id would fall back to a scan plus sort.
func TestPgEmbeddingSearch_UsesPartialIndexUnderGenericPlan(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	spec := pgEnsure(t, drv, "plan@1", 4)
	index, _ := pgdrv.EmbeddingIndexNamesForTest(spec.ModelID)

	tx, err := drv.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// A named prepared statement under force_generic_plan is planned
	// without parameter values — the worst case for a partial index. Only
	// scans that cannot serve an ordered KNN are disabled, so the planner
	// still picks between the partial HNSW index and nothing.
	for _, stmt := range []string{
		`SET LOCAL plan_cache_mode = force_generic_plan`,
		`SET LOCAL enable_seqscan = off`,
		`SET LOCAL enable_bitmapscan = off`,
		`SET LOCAL enable_sort = off`,
		`PREPARE emb_knn_probe AS ` + pgdrv.EmbeddingSearchQueryForTest(spec.ModelID, 4),
	} {
		if _, err := tx.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	plan := explainLines(t, tx, `EXPLAIN EXECUTE emb_knn_probe('[1,0,0,0]', 10)`)
	// PREPARE is session state, not transactional: release it explicitly.
	if _, err := tx.Exec(`DEALLOCATE emb_knn_probe`); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(plan, "\n"); !strings.Contains(joined, "Index Scan using "+index) {
		t.Errorf("KNN query not served by %s under a generic plan:\n%s", index, joined)
	}
}

// TestPgEmbeddingIndex_CheckBacksUpPut pins the database-level dimension
// guard: a wrong-dimension row written around Put is rejected.
func TestPgEmbeddingIndex_CheckBacksUpPut(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	spec := pgEnsure(t, drv, "check@1", 4)
	_, check := pgdrv.EmbeddingIndexNamesForTest(spec.ModelID)
	pgInsertObject(t, drv, "c-1")
	// Drop the index so only the CHECK stands between the row and the table.
	index, _ := pgdrv.EmbeddingIndexNamesForTest(spec.ModelID)
	if _, err := drv.DB().Exec(`DROP INDEX ` + index); err != nil {
		t.Fatal(err)
	}
	_, err := drv.DB().Exec(`
		INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at)
		VALUES ('c-1', $1, 0, '[1,0,0]'::vector, NOW())`, spec.ModelID)
	if err == nil || !strings.Contains(err.Error(), check) {
		t.Errorf("wrong-dimension insert = %v, want violation of %s", err, check)
	}
}

// TestPgEmbeddingIndex_LiveDriftRebuilds covers drift the stamp cannot
// see: an index rebuilt by hand with another operator class, or a dropped
// CHECK, while the stamp still matches.
func TestPgEmbeddingIndex_LiveDriftRebuilds(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	spec := pgEnsure(t, drv, "live@1", 4)
	index, check := pgdrv.EmbeddingIndexNamesForTest(spec.ModelID)
	for _, stmt := range []string{
		`DROP INDEX ` + index,
		`CREATE INDEX ` + index + ` ON embeddings USING hnsw ((vector::vector(4)) vector_l2_ops) WHERE model_id = 'live@1'`,
		`ALTER TABLE embeddings DROP CONSTRAINT ` + check,
	} {
		if _, err := drv.DB().Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := drv.Embeddings().EnsureIndex(ctx, spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	live, _ := indexsig.PostgresVectorIndexFor(ctx, drv.DB(), index)
	if live.OpsClass != "vector_cosine_ops" {
		t.Errorf("live index after rebuild: %+v", live)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_constraint WHERE conname = $1`, check); n != 1 {
		t.Error("dropped CHECK not restored")
	}
}

// TestPgEmbeddingIndex_MismatchedRowsBlockRebuild pins that a rebuild
// refuses canonical rows whose dimension disagrees with the spec and
// leaves the previous state in place.
func TestPgEmbeddingIndex_MismatchedRowsBlockRebuild(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	spec := pgEnsure(t, drv, "mismatch@1", 4)
	pgInsertObject(t, drv, "m-1")
	if err := drv.Embeddings().Put(ctx, "m-1", []storage.ObjectVector{{ModelID: spec.ModelID, Vector: []float32{1, 0, 0, 0}}}); err != nil {
		t.Fatal(err)
	}
	grown := spec
	grown.Dimension = 8
	if err := drv.Embeddings().EnsureIndex(ctx, grown); !errors.Is(err, storage.ErrEmbeddingDimension) {
		t.Fatalf("EnsureIndex over mismatched rows = %v, want ErrEmbeddingDimension", err)
	}
	hits, err := drv.Embeddings().Search(ctx, storage.VectorQuery{ModelID: spec.ModelID, Vector: []float32{1, 0, 0, 0}})
	if err != nil || len(hits) != 1 {
		t.Errorf("failed rebuild damaged the index: %+v, %v", hits, err)
	}
}

// TestPgMigration14_UpgradedDatabase rewinds a migrated database to the
// pre-14 shape (no object FK, placeholder seeded, no per-model indexes) with
// a placeholder row, a live row and an orphaned row, then migrates.
func TestPgMigration14_UpgradedDatabase(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	db := drv.DB()
	const legacy = "legacy-blob-4@2026-05-07"
	for _, stmt := range []string{
		`ALTER TABLE embeddings DROP CONSTRAINT embeddings_object_fk`,
		`ALTER TABLE embeddings DROP CONSTRAINT embeddings_chunk_idx_nonneg`,
		`DELETE FROM schema_version WHERE version >= 14`,
		`INSERT INTO embedding_models (model_id, provider, dimension, is_default, registered_at)
		 VALUES ('` + legacy + `', 'legacy-blob', 4, 1, NOW())`,
		`INSERT INTO index_signatures (signature_id, signature_hash, computed_at)
		 VALUES ('embeddings_` + legacy + `', 'x', NOW())`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("rewind: %s: %v", stmt, err)
		}
	}
	pgInsertModel(t, drv, "real@1", "ollama", 4)
	pgInsertObject(t, drv, "keep")
	for _, r := range []struct{ obj, model string }{{"keep", legacy}, {"keep", "real@1"}, {"gone", "real@1"}} {
		if _, err := db.Exec(`
			INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, created_at)
			VALUES ($1, $2, 0, '[1,0,0,0]'::vector, NOW())`, r.obj, r.model); err != nil {
			t.Fatalf("seed %v: %v", r, err)
		}
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM embedding_models WHERE provider = 'legacy-blob'`); n != 0 {
		t.Errorf("placeholder survived (%d)", n)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM index_signatures WHERE signature_id = $1`, "embeddings_"+legacy); n != 0 {
		t.Error("placeholder signature survived")
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM embeddings`); n != 1 {
		t.Errorf("rows after migrate: %d, want only (keep, real@1)", n)
	}
	hits, err := drv.Embeddings().Search(ctx, storage.VectorQuery{ModelID: "real@1", Vector: []float32{1, 0, 0, 0}})
	if err != nil || len(hits) != 1 || hits[0].ObjectID != "keep" {
		t.Errorf("migrated row not searchable: %+v, %v", hits, err)
	}
	for _, c := range []string{"embeddings_object_fk", "embeddings_chunk_idx_nonneg"} {
		if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_constraint WHERE conname = $1`, c); n != 1 {
			t.Errorf("constraint %s missing", c)
		}
	}
	if _, err := db.Exec(`DELETE FROM objects WHERE id = 'keep'`); err != nil {
		t.Fatal(err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM embeddings`); n != 0 {
		t.Error("object delete did not cascade")
	}

	// Reopen is idempotent.
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

// TestPgEmbeddingIndex_BuiltOnOpen pins the post-migrate pass: a model
// registered without an index gets one on the next Migrate, and models that
// cannot be indexed are skipped without failing it.
func TestPgEmbeddingIndex_BuiltOnOpen(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	pgInsertModel(t, drv, "late@1", "test", 4)
	pgInsertModel(t, drv, "bad id", "test", 4)
	pgInsertModel(t, drv, "huge@1", "test", 3000)
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate with unindexable models: %v", err)
	}
	idx, _ := pgdrv.EmbeddingIndexNamesForTest("late@1")
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = $1`, idx); n != 1 {
		t.Error("index for late@1 not built on open")
	}
	for _, id := range []string{"bad id", "huge@1"} {
		idx, _ := pgdrv.EmbeddingIndexNamesForTest(id)
		if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = $1`, idx); n != 0 {
			t.Errorf("index built for unindexable model %q", id)
		}
	}
	if err := drv.Embeddings().PurgeModel(ctx, "bad id"); err != nil {
		t.Errorf("PurgeModel(invalid id): %v", err)
	}
}

func explainLines(t *testing.T, tx *sql.Tx, query string) []string {
	t.Helper()
	rows, err := tx.Query(query)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return plan
}
