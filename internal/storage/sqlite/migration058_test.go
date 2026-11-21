package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMigration058_AmbientSession upgrades a schema-57 database: the
// columns and the partial index appear, existing objects keep a NULL
// session id, and the migration is re-runnable.
func TestMigration058_AmbientSession(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "m58.db")

	before := openAtVersion(t, path, 57)
	if n := scalarInt(t, before, `SELECT COUNT(*) FROM pragma_table_info('objects') WHERE name = 'session_id'`); n != 0 {
		t.Fatalf("schema 57 already has objects.session_id")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := before.db.Exec(`INSERT INTO objects (id, type, raw_content, text_content, created_at, updated_at)
		VALUES ('pre', 'note', 'raw', 'text', ?, ?)`, now, now); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := before.Close(ctx); err != nil {
		t.Fatal(err)
	}

	d, err := New(path, testBlobStore(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close(ctx)
	if err := d.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('objects') WHERE name = 'session_id'`); n != 1 {
		t.Errorf("objects.session_id missing after migration")
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('jobs') WHERE name = 'user_ambient'`); n != 1 {
		t.Errorf("jobs.user_ambient missing after migration")
	}
	var def string
	if err := d.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = 'idx_objects_session'`).Scan(&def); err != nil {
		t.Fatalf("idx_objects_session missing: %v", err)
	}
	if !strings.Contains(strings.ToLower(def), "where session_id is not null") {
		t.Errorf("index %q is not partial over non-null session ids", def)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM objects WHERE id = 'pre' AND session_id IS NULL`); n != 1 {
		t.Errorf("pre-existing object lost its NULL session id")
	}
	// Re-run: every step is idempotent.
	if err := migrate058AmbientSession(ctx, d); err != nil {
		t.Errorf("re-run: %v", err)
	}
}
