//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
)

// schema14Driver opens a fresh database migrated only through version 14:
// objects.embedding (with its HNSW index) and objects.vector_indexed are
// still present.
func schema14Driver(t *testing.T) *pgdrv.Driver {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn)
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(context.Background(), 14); err != nil {
		t.Fatalf("migrate through 14: %v", err)
	}
	return drv
}

// pgLegacyArtifacts lists what migration 15 must remove.
func pgLegacyArtifacts(t *testing.T, drv *pgdrv.Driver) []string {
	t.Helper()
	var found []string
	for _, col := range []string{"embedding", "vector_indexed"} {
		if pgColumnExists(t, drv.DB(), "objects", col) {
			found = append(found, "column objects."+col)
		}
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'objects' AND indexname LIKE 'idx_objects_embedding%'`); n != 0 {
		found = append(found, "index idx_objects_embedding*")
	}
	return found
}

// TestPgMigration15_UpgradedDatabase migrates a schema-14 database holding
// legacy single-vector data: objects.embedding, its HNSW index and
// objects.vector_indexed are gone, objects keep their content, and the
// per-model embeddings, index and stamp are untouched.
func TestPgMigration15_UpgradedDatabase(t *testing.T) {
	drv := schema14Driver(t)
	ctx := context.Background()
	db := drv.DB()

	if got := pgLegacyArtifacts(t, drv); len(got) != 3 {
		t.Fatalf("schema 14 lacks the legacy artifacts this test removes: %v", got)
	}
	legacyVec := "[1" + strings.Repeat(",0", 1535) + "]"
	for _, o := range []struct {
		id      string
		indexed bool
	}{{"legacy", true}, {"plain", false}} {
		if _, err := db.Exec(`
			INSERT INTO objects (id, type, raw_content, embedding, vector_indexed, created_at, updated_at)
			VALUES ($1, 'note', $2, $3::vector, $4, NOW(), NOW())`,
			o.id, "raw "+o.id, legacyVec, o.indexed); err != nil {
			t.Fatalf("seed object %s: %v", o.id, err)
		}
	}
	spec := pgEnsure(t, drv, "real@1", 4)
	if err := drv.Embeddings().Put(ctx, "legacy", []storage.ObjectVector{
		{ModelID: spec.ModelID, Vector: []float32{1, 0, 0, 0}, Text: "body"},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sigID := indexsig.EmbeddingSignatureID(spec.ModelID)
	sigBefore, err := indexsig.Load(ctx, db, indexsig.DialectPostgres, sigID)
	if err != nil || sigBefore == nil {
		t.Fatalf("pre-15 stamp: %+v, %v", sigBefore, err)
	}

	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if got := pgLegacyArtifacts(t, drv); len(got) != 0 {
		t.Errorf("legacy artifacts survived 15: %v", got)
	}
	assertPgObjectsKeptContent(t, drv, "legacy", "plain")
	assertPgPerModelUntouched(t, drv, spec.ModelID, sigBefore.SignatureHash)

	// Writes still work against the narrowed objects table.
	if err := drv.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "after", Type: "note", RawContent: "new", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create after 15: %v", err)
	}

	// Reopen is idempotent.
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT MAX(version) FROM schema_version`); n != pgdrv.LatestSchemaVersionForTest() {
		t.Errorf("ledger at %d, want %d", n, pgdrv.LatestSchemaVersionForTest())
	}
}

// assertPgObjectsKeptContent checks each seeded object still reads back
// with its raw content.
func assertPgObjectsKeptContent(t *testing.T, drv *pgdrv.Driver, ids ...string) {
	t.Helper()
	for _, id := range ids {
		obj, err := drv.Objects().Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%s) after 15: %v", id, err)
		}
		if obj.RawContent != "raw "+id {
			t.Errorf("%s raw_content = %q", id, obj.RawContent)
		}
	}
}

// assertPgPerModelUntouched checks the seeded model's row, index and stamp
// survived 15 unchanged.
func assertPgPerModelUntouched(t *testing.T, drv *pgdrv.Driver, modelID, sigHash string) {
	t.Helper()
	ctx := context.Background()
	var obj, model, text string
	if err := drv.DB().QueryRow(`SELECT object_id, model_id, text FROM embeddings`).Scan(&obj, &model, &text); err != nil {
		t.Fatalf("per-model row: %v", err)
	}
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM embeddings`); n != 1 || obj != "legacy" || model != modelID || text != "body" {
		t.Errorf("per-model rows: n=%d first=(%s,%s,%s)", n, obj, model, text)
	}
	index, _ := pgdrv.EmbeddingIndexNamesForTest(modelID)
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = $1`, index); n != 1 {
		t.Errorf("per-model index %s missing after 15", index)
	}
	hits, err := drv.Embeddings().Search(ctx, storage.VectorQuery{ModelID: modelID, Vector: []float32{1, 0, 0, 0}})
	if err != nil || len(hits) != 1 || hits[0].ObjectID != "legacy" {
		t.Errorf("per-model search after 15: %+v, %v", hits, err)
	}
	sig, err := indexsig.Load(ctx, drv.DB(), indexsig.DialectPostgres, indexsig.EmbeddingSignatureID(modelID))
	if err != nil || sig == nil || sig.SignatureHash != sigHash {
		t.Errorf("15 changed the per-model stamp: %+v, want hash %s (%v)", sig, sigHash, err)
	}
}

// TestPgMigration15_FreshInstall pins that a fresh database carries none of
// the single-vector artifacts.
func TestPgMigration15_FreshInstall(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	if got := pgLegacyArtifacts(t, drv); len(got) != 0 {
		t.Errorf("fresh install carries legacy artifacts: %v", got)
	}
}
