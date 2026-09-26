package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// openAtVersion creates a database at path migrated only through version,
// the schema an install that stopped at that release still carries.
func openAtVersion(t *testing.T, path string, version int) *Driver {
	t.Helper()
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.migrateThrough(context.Background(), version); err != nil {
		t.Fatalf("migrate through %d: %v", version, err)
	}
	return d
}

// latestMigrationVersion is the version a fully migrated ledger ends at.
func latestMigrationVersion() int {
	return migrations[len(migrations)-1].Version
}

// legacyArtifacts lists what migration 038 must remove, as found in
// sqlite_master and pragma_table_info.
func legacyArtifacts(t *testing.T, d *Driver) []string {
	t.Helper()
	found := legacySchemaObjects(t, d)
	for _, col := range []string{"embeddings", "vector_indexed"} {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('objects') WHERE name = ?`, col); n != 0 {
			found = append(found, "column objects."+col)
		}
	}
	return found
}

// legacySchemaObjects lists the sqlite_master entries of the single-vector
// path: the two tables, the legacy index, and vec0's shadow tables.
func legacySchemaObjects(t *testing.T, d *Driver) []string {
	t.Helper()
	rows, err := d.db.Query(`
		SELECT type || ' ' || name FROM sqlite_master
		 WHERE name IN ('vec_objects', 'object_embeddings', 'idx_object_embeddings_id')
		    OR name LIKE 'vec\_objects\_%' ESCAPE '\'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		found = append(found, s)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// seedPre038Database creates a database at schema 37 holding legacy
// single-vector data (objects.embeddings, objects.vector_indexed,
// object_embeddings and vec_objects rows) next to a registered model with
// a per-model embedding row.
func seedPre038Database(t *testing.T, path string) {
	t.Helper()
	ctx := context.Background()
	d := openAtVersion(t, path, 37)

	if got := legacyArtifacts(t, d); len(got) < 5 {
		t.Fatalf("schema 37 lacks the legacy artifacts this test removes: %v", got)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	legacyVec := make([]float32, legacyVectorDimension)
	legacyVec[0] = 1
	for _, o := range []struct {
		id      string
		indexed int
	}{{"legacy", 1}, {"plain", 0}} {
		if _, err := d.db.Exec(`
			INSERT INTO objects (id, type, raw_content, text_content, embeddings, vector_indexed, created_at, updated_at)
			VALUES (?, 'note', ?, ?, ?, ?, ?, ?)`,
			o.id, "raw "+o.id, "text "+o.id, vecBlob(t, 1, 0, 0, 0), o.indexed, now, now); err != nil {
			t.Fatalf("seed object %s: %v", o.id, err)
		}
	}
	if _, err := d.db.Exec(`
		INSERT INTO object_embeddings (id, embedding, dimensions, model) VALUES ('legacy', ?, ?, 'old')`,
		vecBlob(t, legacyVec...), legacyVectorDimension); err != nil {
		t.Fatalf("seed object_embeddings: %v", err)
	}
	if _, err := d.db.Exec(`INSERT INTO vec_objects (id, embedding) VALUES ('legacy', ?)`,
		vecBlob(t, legacyVec...)); err != nil {
		t.Fatalf("seed vec_objects: %v", err)
	}

	insertModelRow(t, d, "real@1", "ollama", 4)
	spec := storage.EmbeddingModelSpec{ModelID: "real@1", Provider: "ollama", Dimension: 4}
	if err := d.Embeddings().EnsureIndex(ctx, spec); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := d.Embeddings().Put(ctx, "legacy", []storage.ObjectVector{
		{ModelID: "real@1", Vector: []float32{1, 0, 0, 0}, Text: "body"},
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestMigration038_UpgradedDatabase migrates a schema-37 database holding
// legacy single-vector data: the legacy tables and columns are gone, the
// objects keep every other column, and per-model embeddings, their index
// and their stamp are untouched.
func TestMigration038_UpgradedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	ctx := context.Background()
	seedPre038Database(t, path)

	before := openAtVersion(t, path, 37)
	sigBefore, err := LoadIndexSignature(ctx, before.db, EmbeddingSignatureID("real@1"))
	if err != nil || sigBefore == nil {
		t.Fatalf("pre-038 stamp: %+v, %v", sigBefore, err)
	}
	if err := before.Close(ctx); err != nil {
		t.Fatal(err)
	}

	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	if err := d.Init(ctx); err != nil {
		t.Fatalf("migrate upgraded db: %v", err)
	}

	if got := legacyArtifacts(t, d); len(got) != 0 {
		t.Errorf("legacy artifacts survived 038: %v", got)
	}
	assertObjectsKeptContent(t, d, "legacy", "plain")
	assertPerModelUntouched(t, d, sigBefore.SignatureHash)

	// Writes still work against the narrowed objects table.
	if err := d.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "after", Type: "note", RawContent: "new", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create after 038: %v", err)
	}

	// Re-open and re-run 038 itself: no error, nothing changes.
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if err := migrate038DropSingleVectorPath(ctx, d); err != nil {
		t.Fatalf("re-run 038: %v", err)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("ledger at %d, want %d", n, latestMigrationVersion())
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM objects`); n != 3 {
		t.Errorf("objects after re-run: %d, want 3", n)
	}
}

// assertObjectsKeptContent checks each seeded object still reads back with
// its raw and text content.
func assertObjectsKeptContent(t *testing.T, d *Driver, ids ...string) {
	t.Helper()
	for _, id := range ids {
		obj, err := d.Objects().Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%s) after 038: %v", id, err)
		}
		if obj.RawContent != "raw "+id || obj.TextContent != "text "+id {
			t.Errorf("%s content changed: raw=%q text=%q", id, obj.RawContent, obj.TextContent)
		}
	}
}

// assertPerModelUntouched checks the seeded real@1 row, its index and its
// stamp survived 038 unchanged.
func assertPerModelUntouched(t *testing.T, d *Driver, sigHash string) {
	t.Helper()
	ctx := context.Background()
	var obj, model, text string
	if err := d.db.QueryRow(`SELECT object_id, model_id, text FROM embeddings`).Scan(&obj, &model, &text); err != nil {
		t.Fatalf("per-model row: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embeddings`); n != 1 || obj != "legacy" || model != "real@1" || text != "body" {
		t.Errorf("per-model rows: n=%d first=(%s,%s,%s), want only (legacy, real@1, body)", n, obj, model, text)
	}
	hits, err := d.Embeddings().Search(ctx, storage.VectorQuery{ModelID: "real@1", Vector: []float32{1, 0, 0, 0}})
	if err != nil || len(hits) != 1 || hits[0].ObjectID != "legacy" {
		t.Errorf("per-model index after 038: %+v, %v", hits, err)
	}
	sig, err := LoadIndexSignature(ctx, d.db, EmbeddingSignatureID("real@1"))
	if err != nil || sig == nil || sig.SignatureHash != sigHash {
		t.Errorf("038 changed the per-model stamp: %+v, want hash %s (%v)", sig, sigHash, err)
	}
}

// TestMigration038_FreshInstall pins that a fresh database carries none of
// the single-vector artifacts.
func TestMigration038_FreshInstall(t *testing.T) {
	d := newTestDriver(t)
	if got := legacyArtifacts(t, d); len(got) != 0 {
		t.Errorf("fresh install carries legacy artifacts: %v", got)
	}
}

// TestMigration038_DependentFailsLoudly pins the guard: a view that still
// reads objects.vector_indexed fails the migration naming the view,
// instead of DROP COLUMN failing on a schema error, and leaves the
// transaction's earlier drops rolled back.
func TestMigration038_DependentFailsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dependent.db")
	ctx := context.Background()
	d := openAtVersion(t, path, 37)
	t.Cleanup(func() { d.Close(ctx) })
	if _, err := d.db.Exec(`CREATE VIEW indexed_objects AS SELECT id FROM objects WHERE vector_indexed = 1`); err != nil {
		t.Fatal(err)
	}
	err := d.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "view indexed_objects") {
		t.Fatalf("Migrate with a dependent view: %v, want it to name view indexed_objects", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE name = 'vec_objects'`); n != 1 {
		t.Error("a failed 038 still dropped vec_objects: the transaction did not roll back")
	}
}
