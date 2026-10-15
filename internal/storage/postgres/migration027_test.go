//go:build integration

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// perPrincipalTables are the tables migration 27 gives an owner_principal
// column, created by that migration on Postgres.
var perPrincipalTables = []string{"search_history", "saved_searches", "resurfacing_queue"}

// TestPgMigration027_OwnerPrincipal pins that a fresh database and one
// migrated from schema 26 both end with owner_principal TEXT NOT NULL
// DEFAULT 'owner' on every per-principal table, and that replaying the
// chain on a migrated database is a no-op.
func TestPgMigration027_OwnerPrincipal(t *testing.T) {
	ctx := context.Background()

	t.Run("fresh install", func(t *testing.T) {
		drv, _ := freshIntegrationDriver(t)
		assertOwnerPrincipalColumns(t, drv.DB())
	})

	t.Run("upgrade from 26", func(t *testing.T) {
		dsn, _ := freshDatabaseDSN(t)
		old, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
		if err != nil {
			t.Fatalf("postgres.New: %v", err)
		}
		if err := old.MigrateThroughForTest(ctx, 26); err != nil {
			t.Fatalf("migrate through 26: %v", err)
		}
		for _, table := range perPrincipalTables {
			if pgTableExists(t, old.DB(), table) {
				t.Fatalf("schema 26 already has %s: nothing for 27 to create", table)
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
		assertOwnerPrincipalColumns(t, drv.DB())

		if err := drv.Migrate(ctx); err != nil {
			t.Fatalf("replay Migrate: %v", err)
		}
		assertOwnerPrincipalColumns(t, drv.DB())
	})
}

// pgTableExists reports whether table exists in the current schema.
func pgTableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (
		SELECT 1 FROM pg_catalog.pg_tables
		 WHERE schemaname = current_schema() AND tablename = $1)`, table).Scan(&exists); err != nil {
		t.Fatalf("inspect %s: %v", table, err)
	}
	return exists
}

// assertOwnerPrincipalColumns checks that every per-principal table has
// owner_principal NOT NULL with the 'owner' default.
func assertOwnerPrincipalColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range perPrincipalTables {
		var nullable string
		var def sql.NullString
		err := db.QueryRow(`SELECT is_nullable, column_default FROM information_schema.columns
			WHERE table_schema = current_schema() AND table_name = $1 AND column_name = 'owner_principal'`,
			table).Scan(&nullable, &def)
		if errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s.owner_principal missing", table)
			continue
		}
		if err != nil {
			t.Fatalf("inspect %s.owner_principal: %v", table, err)
		}
		if nullable != "NO" || !strings.Contains(def.String, "'owner'") {
			t.Errorf("%s.owner_principal: nullable %q default %q, want NOT NULL DEFAULT 'owner'", table, nullable, def.String)
		}
	}
}
