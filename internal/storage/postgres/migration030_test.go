//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// objectListIndexes are the indexes migration 30 creates.
var objectListIndexes = []string{"idx_objects_status", "idx_objects_created_at", "idx_objects_updated_at"}

// objectIndexRows is how many pg_indexes rows name index on objects.
func objectIndexRows(t *testing.T, drv *pgdrv.Driver, index string) int {
	t.Helper()
	return pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'objects' AND indexname = $1`, index)
}

// assertObjectListIndexes checks that every list index exists once.
func assertObjectListIndexes(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	for _, index := range objectListIndexes {
		if n := objectIndexRows(t, drv, index); n != 1 {
			t.Errorf("index %s: %d rows in pg_indexes, want 1", index, n)
		}
	}
}

// TestPgMigration030_ObjectListIndexes pins that a fresh database and one
// migrated from schema 28 both carry the list indexes, and that replaying
// the chain is a no-op.
func TestPgMigration030_ObjectListIndexes(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertObjectListIndexes(t, drv)
	})

	t.Run("upgrade from 28", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 28); err != nil {
			t.Fatalf("migrate through 28: %v", err)
		}
		for _, index := range objectListIndexes {
			if objectIndexRows(t, old, index) != 0 {
				t.Fatalf("schema 28 already has %s: nothing for 30 to create", index)
			}
		}
		if err := old.Close(ctx); err != nil {
			t.Fatal(err)
		}

		drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		t.Cleanup(func() { drv.Close(ctx) })
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
		assertObjectListIndexes(t, drv)
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		assertObjectListIndexes(t, drv)
		var version sql.NullInt64
		if err := drv.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if int(version.Int64) != pgdrv.LatestSchemaVersionForTest() {
			t.Errorf("schema_version after migrate = %d, want %d", version.Int64, pgdrv.LatestSchemaVersionForTest())
		}
	})
}
