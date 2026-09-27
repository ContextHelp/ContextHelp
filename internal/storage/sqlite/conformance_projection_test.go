package sqlite

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProjectionStore runs the cross-driver re-projection
// contract against a fresh SQLite database.
func TestConformance_ProjectionStore(t *testing.T) {
	d := newTestDriver(t)
	storagetest.ProjectionStoreConformance(t, d, storagetest.ProjectionDB{DB: d.db, Dialect: indexsig.DialectSQLite})
}

// TestMigrateProjectionVersion_InvalidatesFTSSignature pins the migration
// that adds objects.projection_version: rows written before it carry no
// stamp, so the stored FTS signature no longer describes them. The
// migration clears the signature hash; the next start sees a mismatch and
// schedules the re-projection, which stamps it again.
func TestMigrateProjectionVersion_InvalidatesFTSSignature(t *testing.T) {
	d := newTestDriver(t)
	ctx := context.Background()
	if _, err := indexsig.VerifyFTS(ctx, d.db, indexsig.DialectSQLite); err != nil {
		t.Fatal(err)
	}
	// Rewind to the pre-040 schema.
	if _, err := d.db.ExecContext(ctx, `ALTER TABLE objects DROP COLUMN projection_version`); err != nil {
		t.Fatal(err)
	}
	if err := migrate040ProjectionVersion(ctx, d); err != nil {
		t.Fatalf("migration: %v", err)
	}
	res, err := indexsig.VerifyFTS(ctx, d.db, indexsig.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match || res.FirstBoot || res.OldHash != "" {
		t.Fatalf("after migration: %+v, want a mismatch against the cleared hash", res)
	}
	// Re-running on a migrated database (column present) changes nothing.
	if err := indexsig.StampFTS(ctx, d.db, indexsig.DialectSQLite); err != nil {
		t.Fatal(err)
	}
	if err := migrate040ProjectionVersion(ctx, d); err != nil {
		t.Fatalf("second run: %v", err)
	}
	res, err = indexsig.VerifyFTS(ctx, d.db, indexsig.DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Match {
		t.Errorf("second run cleared the signature again: %+v", res)
	}
}
