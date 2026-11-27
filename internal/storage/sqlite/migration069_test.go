package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration069_DerivationSig migrates a schema-68 database: the column
// appears unsigned on existing rows, with its index, and a replay is a
// no-op that keeps a stamped value.
func TestMigration069_DerivationSig(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v68.db"), 68)
	t.Cleanup(func() { d.Close(ctx) })
	if hasColumn(t, d, "objects", "derivation_sig") {
		t.Fatal("schema 68 already has objects.derivation_sig: nothing for 069 to add")
	}
	if _, err := d.db.ExecContext(ctx,
		`INSERT INTO objects (id, type, created_at, updated_at, pipeline) VALUES ('old', 'note', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z', 'text@v0')`); err != nil {
		t.Fatal(err)
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	check := func(stage, want string) {
		t.Helper()
		if !hasColumn(t, d, "objects", "derivation_sig") {
			t.Fatalf("%s: objects.derivation_sig missing", stage)
		}
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_objects_derivation_sig'`); n != 1 {
			t.Errorf("%s: idx_objects_derivation_sig missing", stage)
		}
		var got string
		if err := d.db.QueryRowContext(ctx, `SELECT derivation_sig FROM objects WHERE id = 'old'`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("%s: existing row derivation_sig = %q, want %q", stage, got, want)
		}
	}
	check("migrated", "")
	if _, err := d.db.ExecContext(ctx, `UPDATE objects SET derivation_sig = 'text#abc' WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate069DerivationSig(ctx, d); err != nil {
		t.Fatalf("re-run 069: %v", err)
	}
	check("replayed", "text#abc")
}
