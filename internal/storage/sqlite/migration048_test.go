package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// authRegistryTables are the tables migration 048 creates.
var authRegistryTables = []string{"principals", "principal_identities", "auth_tickets", "kv"}

// hasTable reports whether sqlite_master lists table.
func hasTable(t *testing.T, d *Driver, table string) bool {
	t.Helper()
	return scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table) != 0
}

// assertAuthRegistry checks that every registry table exists and that
// exactly one owner principal row is seeded, active and admin.
func assertAuthRegistry(t *testing.T, d *Driver) {
	t.Helper()
	for _, table := range authRegistryTables {
		if !hasTable(t, d, table) {
			t.Errorf("table %s missing", table)
		}
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM principals WHERE kind = ?`, storage.PrincipalKindOwner); n != 1 {
		t.Errorf("owner rows: %d, want exactly 1", n)
	}
	owner, err := d.Auth().Principals().Get(context.Background(), storage.OwnerPrincipal)
	if err != nil {
		t.Fatalf("Get(owner): %v", err)
	}
	if owner.Kind != storage.PrincipalKindOwner || owner.Status != storage.PrincipalActive ||
		owner.DisplayName != "Owner" || len(owner.Roles) != 1 || owner.Roles[0] != "admin" {
		t.Errorf("owner = %+v, want kind owner, active, display name Owner, roles [admin]", owner)
	}
}

// TestMigration048_AuthRegistry migrates a schema-47 database: the
// registry tables appear, the owner row is seeded once, re-running 048
// is a no-op (the owner keeps its row and display name) and the ledger
// ends at the latest version.
func TestMigration048_AuthRegistry(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v47.db"), 47)
	t.Cleanup(func() { d.Close(ctx) })

	for _, table := range authRegistryTables {
		if hasTable(t, d, table) {
			t.Fatalf("schema 47 already has %s: nothing for 048 to create", table)
		}
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertAuthRegistry(t, d)

	// A renamed owner survives a replay: the seed is ON CONFLICT DO NOTHING.
	if _, err := d.db.ExecContext(ctx, `UPDATE principals SET display_name = 'Jad' WHERE id = ?`, storage.OwnerPrincipal); err != nil {
		t.Fatalf("rename owner: %v", err)
	}
	if err := migrate048AuthRegistry(ctx, d); err != nil {
		t.Fatalf("re-run 048: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM principals`); n != 1 {
		t.Errorf("principals after re-run: %d, want 1", n)
	}
	owner, err := d.Auth().Principals().Get(ctx, storage.OwnerPrincipal)
	if err != nil || owner.DisplayName != "Jad" {
		t.Errorf("owner after re-run = %+v, %v; want the renamed row kept", owner, err)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM schema_version WHERE version = 48`); n != 1 {
		t.Errorf("ledger rows for 048: %d, want 1", n)
	}
}

// TestMigration048_FreshInstall pins that a fresh database carries the
// registry tables and the seeded owner.
func TestMigration048_FreshInstall(t *testing.T) {
	assertAuthRegistry(t, newTestDriver(t))
}
