package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// hasObjectsColumn reports whether pragma_table_info('objects') lists col.
func hasObjectsColumn(t *testing.T, d *Driver, col string) bool {
	t.Helper()
	return scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('objects') WHERE name = ?`, col) != 0
}

// TestMigration045_DropsObjectsPlugins migrates a schema-44 database whose
// objects carry a filled plugins column: the column is gone, the objects
// keep their content, writes still work, the FTS signature is unchanged
// (no re-projection), and re-running 045 is a no-op.
func TestMigration045_DropsObjectsPlugins(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v44.db"), 44)
	t.Cleanup(func() { d.Close(ctx) })

	if !hasObjectsColumn(t, d, "plugins") {
		t.Fatal("schema 44 lacks objects.plugins: nothing for 045 to drop")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"filled", "empty"} {
		plugins := `{}`
		if id == "filled" {
			plugins = `{"autosuggest":{"pending":["x"]}}`
		}
		if _, err := d.db.ExecContext(ctx, `
			INSERT INTO objects (id, type, raw_content, text_content, plugins, created_at, updated_at)
			VALUES (?, 'note', ?, ?, ?, ?, ?)`,
			id, "raw "+id, "text "+id, plugins, now, now); err != nil {
			t.Fatalf("seed %s at v44: %v", id, err)
		}
	}
	ftsBefore, _, err := indexsig.ComputeFTS(ctx, d.db, indexsig.DialectSQLite)
	if err != nil {
		t.Fatalf("FTS signature at v44: %v", err)
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if hasObjectsColumn(t, d, "plugins") {
		t.Error("objects.plugins survived 045")
	}
	assertObjectsKeptContent(t, d, "filled", "empty")
	if err := d.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "after", Type: "note", RawContent: "new", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Create after 045: %v", err)
	}
	ftsAfter, _, err := indexsig.ComputeFTS(ctx, d.db, indexsig.DialectSQLite)
	if err != nil {
		t.Fatalf("FTS signature after 045: %v", err)
	}
	if ftsAfter != ftsBefore {
		t.Error("045 changed the FTS signature: dropping objects.plugins must not schedule a re-projection")
	}

	if err := migrate045DropObjectsPlugins(ctx, d); err != nil {
		t.Fatalf("re-run 045: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM objects`); n != 3 {
		t.Errorf("objects after re-run: %d, want 3", n)
	}
}

// TestMigration045_FreshInstall pins that a fresh database has no
// objects.plugins column.
func TestMigration045_FreshInstall(t *testing.T) {
	d := newTestDriver(t)
	if hasObjectsColumn(t, d, "plugins") {
		t.Error("fresh install carries objects.plugins")
	}
}

// TestMigration045_DependentFailsLoudly pins the guard: an index that
// still reads objects.plugins fails the migration naming the index,
// instead of DROP COLUMN failing on a schema error.
func TestMigration045_DependentFailsLoudly(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "dependent.db"), 44)
	t.Cleanup(func() { d.Close(ctx) })
	if _, err := d.db.Exec(`CREATE INDEX idx_objects_plugins ON objects(plugins)`); err != nil {
		t.Fatal(err)
	}
	err := d.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "drop objects.plugins: still referenced by index idx_objects_plugins") {
		t.Fatalf("Migrate with a dependent index: %v, want it to name index idx_objects_plugins", err)
	}
	if !hasObjectsColumn(t, d, "plugins") {
		t.Error("a failed 045 still dropped objects.plugins")
	}
}
