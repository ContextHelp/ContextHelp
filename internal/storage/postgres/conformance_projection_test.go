//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ProjectionStore runs the cross-driver re-projection
// contract against a fresh Postgres database.
func TestConformance_ProjectionStore(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.ProjectionStoreConformance(t, drv, storagetest.ProjectionDB{DB: drv.DB(), Dialect: indexsig.DialectPostgres})
}

// TestPostgresMigrateProjectionVersion_InvalidatesFTSSignature pins the
// migration that adds objects.projection_version: on a database whose
// stamp predates it, the FTS signature hash is cleared, so the next start
// reports a mismatch and schedules the re-projection.
func TestPostgresMigrateProjectionVersion_InvalidatesFTSSignature(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()
	db := drv.DB()
	if _, err := indexsig.VerifyFTS(ctx, db, indexsig.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	// Rewind to the schema before the migration and replay the chain.
	for _, q := range []string{
		`ALTER TABLE objects DROP COLUMN projection_version`,
		`DELETE FROM schema_version WHERE version >= 18`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	res, err := indexsig.VerifyFTS(ctx, db, indexsig.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match || res.FirstBoot || res.OldHash != "" {
		t.Fatalf("after migration: %+v, want a mismatch against the cleared hash", res)
	}

	// Replaying the chain on a migrated database changes nothing.
	if err := indexsig.StampFTS(ctx, db, indexsig.DialectPostgres); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_version WHERE version >= 18`); err != nil {
		t.Fatal(err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if res, err = indexsig.VerifyFTS(ctx, db, indexsig.DialectPostgres); err != nil || !res.Match {
		t.Fatalf("replay cleared the signature again: %+v, %v", res, err)
	}
}
