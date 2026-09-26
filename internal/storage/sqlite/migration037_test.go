package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// rewindTo036 turns a fully migrated database back into the pre-037 shape:
// the composite-key embeddings table from migration 032 with no id column
// and no object FK, no per-model indexes, the legacy-blob placeholder
// seeded by 033, and a ledger that stops at 36.
func rewindTo036(t *testing.T, d *Driver) {
	t.Helper()
	ctx := context.Background()
	specs, err := registeredEmbeddingSpecs(ctx, d.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range specs {
		if err := d.Embeddings().PurgeModel(ctx, sp.ModelID); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		`DROP TABLE embeddings`,
		`CREATE TABLE embeddings (
		    object_id  TEXT NOT NULL,
		    model_id   TEXT NOT NULL REFERENCES embedding_models(model_id),
		    chunk_idx  INTEGER NOT NULL DEFAULT 0,
		    vector     BLOB NOT NULL,
		    text       TEXT,
		    created_at TEXT NOT NULL,
		    PRIMARY KEY (object_id, model_id, chunk_idx)
		)`,
		`CREATE INDEX idx_embeddings_model ON embeddings (model_id, object_id)`,
		`DELETE FROM schema_version WHERE version >= 37`,
	} {
		if _, err := d.db.Exec(stmt); err != nil {
			t.Fatalf("rewind: %s: %v", stmt, err)
		}
	}
	if err := migrate033EmbeddingsBackfill(ctx, d); err != nil {
		t.Fatalf("re-seed placeholder: %v", err)
	}
}

func scalarInt(t *testing.T, d *Driver, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestMigration037_UpgradedDatabase seeds a pre-037 database with a
// placeholder row, a real model's row, and a real model's row whose object
// is gone, then migrates: only the real, live row survives, now indexed.
func TestMigration037_UpgradedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	ctx := context.Background()
	legacy := seedPre037Database(t, path)

	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	if err := d.Init(ctx); err != nil {
		t.Fatalf("migrate upgraded db: %v", err)
	}

	assertPlaceholderGone(t, d, legacy)
	var obj, model, text string
	if err := d.db.QueryRow(`SELECT object_id, model_id, text FROM embeddings`).Scan(&obj, &model, &text); err != nil {
		t.Fatalf("surviving row: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embeddings`); n != 1 || obj != "keep" || model != "real@1" || text != "body" {
		t.Errorf("surviving rows: n=%d first=(%s,%s,%s), want only (keep, real@1, body)", n, obj, model, text)
	}
	hits, err := d.Embeddings().Search(ctx, storage.VectorQuery{ModelID: "real@1", Vector: []float32{1, 0, 0, 0}})
	if err != nil || len(hits) != 1 || hits[0].ObjectID != "keep" {
		t.Errorf("migrated row not indexed: %+v, %v", hits, err)
	}
	assertPerModelEmbeddingsSchema(t, d)
	assertReopenIdempotent(t, d, "real@1")
}

// seedPre037Database creates a database at schema 36 holding a
// placeholder row, a live real row, and an orphaned real row. Returns the
// placeholder's model_id.
func seedPre037Database(t *testing.T, path string) string {
	t.Helper()
	ctx := context.Background()
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	d.SetVectorDimension(4)
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	rewindTo036(t, d)

	legacy := LegacyEmbeddingModelID(4)
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embedding_models WHERE model_id = ?`, legacy); n != 1 {
		t.Fatalf("rewind did not seed the placeholder (%d rows)", n)
	}
	insertModelRow(t, d, "real@1", "ollama", 4)
	insertObjectRow(t, d, "keep")
	now := time.Now().UTC().Format(time.RFC3339)
	for _, r := range []struct{ obj, model string }{
		{"keep", legacy},
		{"keep", "real@1"},
		{"gone", "real@1"}, // no such object: the old table had no FK
	} {
		if _, err := d.db.Exec(`
			INSERT INTO embeddings (object_id, model_id, chunk_idx, vector, text, created_at)
			VALUES (?, ?, 0, ?, 'body', ?)`, r.obj, r.model, vecBlob(t, 1, 0, 0, 0), now); err != nil {
			t.Fatalf("seed %v: %v", r, err)
		}
	}
	if err := d.Close(ctx); err != nil {
		t.Fatal(err)
	}
	return legacy
}

func assertPlaceholderGone(t *testing.T, d *Driver, legacy string) {
	t.Helper()
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embedding_models WHERE provider = 'legacy-blob'`); n != 0 {
		t.Errorf("placeholder model survived: %d rows", n)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM index_signatures WHERE signature_id = ?`, EmbeddingSignatureID(legacy)); n != 0 {
		t.Errorf("placeholder signature survived")
	}
}

// assertReopenIdempotent re-runs Migrate and 037 itself: same ledger,
// rows, and stamp.
func assertReopenIdempotent(t *testing.T, d *Driver, modelID string) {
	t.Helper()
	ctx := context.Background()
	rowsBefore := scalarInt(t, d, `SELECT COUNT(*) FROM embeddings`)
	sigBefore, _ := LoadIndexSignature(ctx, d.db, EmbeddingSignatureID(modelID))
	if sigBefore == nil {
		t.Fatalf("%s not stamped", modelID)
	}
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if err := migrate037PerModelEmbeddings(ctx, d); err != nil {
		t.Fatalf("re-run 037 on migrated schema: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embeddings`); n != rowsBefore {
		t.Errorf("re-run changed rows: %d -> %d", rowsBefore, n)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != 37 {
		t.Errorf("ledger at %d, want 37", n)
	}
	sigAfter, _ := LoadIndexSignature(ctx, d.db, EmbeddingSignatureID(modelID))
	if sigAfter == nil || sigBefore.SignatureHash != sigAfter.SignatureHash {
		t.Errorf("re-open changed the stamp: %+v -> %+v", sigBefore, sigAfter)
	}
}

// assertPerModelEmbeddingsSchema pins the rebuilt table: explicit id
// rowid alias, the logical-key UNIQUE, a non-negative chunk CHECK, and a
// cascading FK to objects.
func assertPerModelEmbeddingsSchema(t *testing.T, d *Driver) {
	t.Helper()
	var ddl string
	if err := d.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'embeddings'`).Scan(&ddl); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"id          INTEGER PRIMARY KEY",
		"REFERENCES objects(id) ON DELETE CASCADE",
		"CHECK (chunk_idx >= 0)",
		"UNIQUE (object_id, model_id, chunk_idx)",
	} {
		if !strings.Contains(ddl, part) {
			t.Errorf("embeddings DDL missing %q:\n%s", part, ddl)
		}
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_embeddings_model'`); n != 1 {
		t.Error("idx_embeddings_model missing")
	}
}

// TestMigration037_FreshInstall pins that a fresh database carries the
// per-model schema and no placeholder model, so there is no default until
// an operator registers one.
func TestMigration037_FreshInstall(t *testing.T) {
	d := newTestDriver(t)
	assertPerModelEmbeddingsSchema(t, d)
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM embedding_models`); n != 0 {
		t.Errorf("fresh install has %d embedding models, want none", n)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM index_signatures WHERE signature_id LIKE 'embeddings\_%' ESCAPE '\'`); n != 0 {
		t.Errorf("fresh install carries %d embedding signatures", n)
	}
}

// TestMigration037_InvalidModelSkipped pins that a pre-existing model whose
// ID cannot be embedded in per-model DDL is skipped, not fatal.
func TestMigration037_InvalidModelSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	ctx := context.Background()
	d, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	rewindTo036(t, d)
	insertModelRow(t, d, "it's-bad", "ollama", 4)
	insertModelRow(t, d, "good@1", "ollama", 4)
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate with an invalid model id: %v", err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	if ok, _ := vecIndexComplete(ctx, d.db, vecIndexFor("good@1")); !ok {
		t.Error("valid model not indexed")
	}
	if ok, _ := vecIndexComplete(ctx, d.db, vecIndexFor("it's-bad")); ok {
		t.Error("invalid model indexed")
	}
}
