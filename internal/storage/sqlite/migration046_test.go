package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestMigration046_ProximityObjectFK migrates a schema-45 database whose
// object_proximity holds a live pair and pairs with a deleted endpoint
// on either side: the orphans are gone, the live pair and the indexes
// stay, deleting an object now drops its pairs, and re-running 046 is a
// no-op.
func TestMigration046_ProximityObjectFK(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v45.db"), 45)
	t.Cleanup(func() { d.Close(ctx) })

	now := time.Now().UTC().Format(time.RFC3339)
	for _, id := range []string{"live-a", "live-b"} {
		if _, err := d.db.ExecContext(ctx, `
			INSERT INTO objects (id, type, raw_content, text_content, created_at, updated_at)
			VALUES (?, 'note', ?, ?, ?, ?)`, id, "raw "+id, "text "+id, now, now); err != nil {
			t.Fatalf("seed %s at v45: %v", id, err)
		}
	}
	for _, pair := range [][2]string{{"live-a", "live-b"}, {"gone-a", "live-b"}, {"live-a", "zz-gone"}} {
		if _, err := d.db.ExecContext(ctx,
			`INSERT INTO object_proximity (object_a, object_b, score) VALUES (?, ?, 0.5)`,
			pair[0], pair[1]); err != nil {
			t.Fatalf("seed pair %v at v45: %v", pair, err)
		}
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if n := scalarInt(t, d, `SELECT COUNT(*) FROM object_proximity`); n != 1 {
		t.Errorf("pairs after 046: %d, want only the live pair", n)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM object_proximity WHERE object_a = 'live-a' AND object_b = 'live-b'`); n != 1 {
		t.Error("live pair dropped by 046")
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'object_proximity' AND name LIKE 'idx_proximity_%'`); n != 4 {
		t.Errorf("proximity indexes after 046: %d, want 4", n)
	}

	if err := migrate046ProximityObjectFK(ctx, d); err != nil {
		t.Fatalf("re-run 046: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM object_proximity`); n != 1 {
		t.Errorf("pairs after re-run: %d, want 1", n)
	}

	if err := d.Objects().Delete(ctx, "live-b"); err != nil {
		t.Fatalf("delete live-b: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM object_proximity`); n != 0 {
		t.Errorf("pairs after deleting live-b: %d, want 0", n)
	}
}
