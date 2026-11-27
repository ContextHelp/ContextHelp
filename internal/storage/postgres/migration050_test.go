//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestPgMigration050_DerivationSig mirrors SQLite 069: an existing row reads
// back unsigned, the index exists, and a replay keeps a stamped value.
func TestPgMigration050_DerivationSig(t *testing.T) {
	ctx := context.Background()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(ctx, 49); err != nil {
		t.Fatalf("migrate through 49: %v", err)
	}
	db := drv.DB()
	if _, err := db.ExecContext(ctx, `INSERT INTO objects (id, type, pipeline, created_at, updated_at) VALUES ('old', 'note', 'text@v0', NOW(), NOW())`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	check := func(stage, want string) {
		t.Helper()
		var got string
		if err := db.QueryRowContext(ctx, `SELECT derivation_sig FROM objects WHERE id = 'old'`).Scan(&got); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if got != want {
			t.Errorf("%s: derivation_sig = %q, want %q", stage, got, want)
		}
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pg_indexes WHERE indexname = 'idx_objects_derivation_sig'`).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: idx_objects_derivation_sig count = %d, %v", stage, n, err)
		}
	}
	check("migrated", "")
	if _, err := db.ExecContext(ctx, `UPDATE objects SET derivation_sig = 'text#abc' WHERE id = 'old'`); err != nil {
		t.Fatal(err)
	}
	if err := drv.ReplayStatementsForTest(ctx, 50); err != nil {
		t.Fatalf("replay 50: %v", err)
	}
	check("replayed", "text#abc")
}
