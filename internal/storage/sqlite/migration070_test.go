package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration070_RederiveTables migrates a schema-69 database: both
// tables appear, and a replay is a no-op that keeps what they hold.
func TestMigration070_RederiveTables(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v69.db"), 69)
	t.Cleanup(func() { d.Close(ctx) })
	for _, table := range []string{"derivation_signatures", "rederive_attempts", "rederive_skips"} {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '`+table+`'`); n != 0 {
			t.Fatalf("schema 69 already has %s: nothing for 070 to add", table)
		}
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.db.ExecContext(ctx,
		`INSERT INTO derivation_signatures (sig, pipeline, entries, recorded_at) VALUES ('p#1', 'p', '["a@1"]', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err := migrate070Rederive(ctx, d); err != nil {
		t.Fatalf("re-run 070: %v", err)
	}
	for _, table := range []string{"derivation_signatures", "rederive_attempts", "rederive_skips"} {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = '`+table+`'`); n != 1 {
			t.Errorf("%s missing after the replay", table)
		}
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM derivation_signatures`); n != 1 {
		t.Errorf("the replay changed derivation_signatures: %d rows", n)
	}
}
