//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// meteringIndexes are the indexes migration 32 creates on metering_events.
var meteringIndexes = []string{
	"idx_metering_events_registry_name",
	"idx_metering_events_occurred_at",
	"idx_metering_events_registry_event",
}

// assertMeteringSchema checks that metering_events exists with each of
// its indexes once.
func assertMeteringSchema(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_tables WHERE tablename = 'metering_events'`); n != 1 {
		t.Fatalf("metering_events: %d rows in pg_tables, want 1", n)
	}
	for _, index := range meteringIndexes {
		if n := pgScalar(t, drv, `SELECT COUNT(*) FROM pg_indexes WHERE tablename = 'metering_events' AND indexname = $1`, index); n != 1 {
			t.Errorf("index %s: %d rows in pg_indexes, want 1", index, n)
		}
	}
}

// TestPgMigration032_MeteringEvents pins that a fresh database and one
// migrated from schema 31 both carry metering_events and that the
// metering store works on it, and that replaying the chain is a no-op.
func TestPgMigration032_MeteringEvents(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertMeteringSchema(t, drv)
	})

	t.Run("upgrade from 31", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 31); err != nil {
			t.Fatalf("migrate through 31: %v", err)
		}
		if n := pgScalar(t, old, `SELECT COUNT(*) FROM pg_tables WHERE tablename = 'metering_events'`); n != 0 {
			t.Fatal("schema 31 already has metering_events: nothing for 32 to create")
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
		assertMeteringSchema(t, drv)
		ok, err := drv.Metering().RecordCapped(ctx, &storage.MeteringEvent{
			ID: "mig32-1", RegistryName: "ops", EventType: storage.MeteringEventEntityResolve,
			Count: 1, OccurredAt: time.Now().UTC(),
		}, time.Time{}, 1)
		if err != nil || !ok {
			t.Fatalf("RecordCapped after upgrade = %v, %v; want true, nil", ok, err)
		}
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		assertMeteringSchema(t, drv)
		var version sql.NullInt64
		if err := drv.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if int(version.Int64) != pgdrv.LatestSchemaVersionForTest() {
			t.Errorf("schema_version after migrate = %d, want %d", version.Int64, pgdrv.LatestSchemaVersionForTest())
		}
	})
}
