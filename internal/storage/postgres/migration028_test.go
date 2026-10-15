//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// authRegistryTables are the tables migration 28 creates.
var authRegistryTables = []string{"principals", "principal_identities", "auth_tickets", "kv"}

// assertAuthRegistry checks that every registry table exists and that
// exactly one owner principal row is seeded, active and admin.
func assertAuthRegistry(t *testing.T, drv *pgdrv.Driver) {
	t.Helper()
	for _, table := range authRegistryTables {
		if !pgTableExists(t, drv.DB(), table) {
			t.Errorf("table %s missing", table)
		}
	}
	var owners int
	if err := drv.DB().QueryRow(`SELECT COUNT(*) FROM principals WHERE kind = $1`, storage.PrincipalKindOwner).Scan(&owners); err != nil {
		t.Fatalf("count owners: %v", err)
	}
	if owners != 1 {
		t.Errorf("owner rows: %d, want exactly 1", owners)
	}
	owner, err := drv.Auth().Principals().Get(context.Background(), storage.OwnerPrincipal)
	if err != nil {
		t.Fatalf("Get(owner): %v", err)
	}
	if owner.Kind != storage.PrincipalKindOwner || owner.Status != storage.PrincipalActive ||
		owner.DisplayName != "Owner" || len(owner.Roles) != 1 || owner.Roles[0] != "admin" {
		t.Errorf("owner = %+v, want kind owner, active, display name Owner, roles [admin]", owner)
	}
}

// TestPgMigration028_AuthRegistry pins that a fresh database and one
// migrated from schema 27 both end with the registry tables and one
// seeded owner, and that replaying the chain keeps a renamed owner.
func TestPgMigration028_AuthRegistry(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertAuthRegistry(t, drv)
	})

	t.Run("upgrade from 27", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 27); err != nil {
			t.Fatalf("migrate through 27: %v", err)
		}
		for _, table := range authRegistryTables {
			if pgTableExists(t, old.DB(), table) {
				t.Fatalf("schema 27 already has %s: nothing for 28 to create", table)
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
		assertAuthRegistry(t, drv)

		if _, err := drv.DB().Exec(`UPDATE principals SET display_name = 'Jad' WHERE id = $1`, storage.OwnerPrincipal); err != nil {
			t.Fatalf("rename owner: %v", err)
		}
		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		var name string
		if err := drv.DB().QueryRow(`SELECT display_name FROM principals WHERE id = $1`, storage.OwnerPrincipal).Scan(&name); err != nil || name != "Jad" {
			t.Errorf("owner after replay = %q, %v; want the renamed row kept", name, err)
		}
		var version sql.NullInt64
		if err := drv.DB().QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
			t.Fatalf("read ledger: %v", err)
		}
		if int(version.Int64) != pgdrv.LatestSchemaVersionForTest() {
			t.Errorf("schema_version after migrate = %d, want %d", version.Int64, pgdrv.LatestSchemaVersionForTest())
		}
	})
}
