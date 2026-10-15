package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// hasTableColumn reports whether pragma_table_info(table) lists col.
func hasTableColumn(t *testing.T, d *Driver, table, col string) bool {
	t.Helper()
	return scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, col) != 0
}

// seedPerPrincipalRows writes one row into each per-principal table of a
// schema-46 database (and the object the queue row references).
func seedPerPrincipalRows(t *testing.T, d *Driver) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO objects (id, type, raw_content, text_content, created_at, updated_at)
		  VALUES ('obj-1', 'note', 'raw', 'text', ?, ?)`, []any{now, now}},
		{`INSERT INTO saved_searches (id, name, query, created_at, updated_at)
		  VALUES ('ss-1', 'legacy', 'type = note', ?, ?)`, []any{now, now}},
		{`INSERT INTO search_history (id, query, searched_at) VALUES ('sh-1', 'legacy query', ?)`, []any{now}},
		{`INSERT INTO resurfacing_queue (id, object_id, profile_id, score, reason)
		  VALUES ('rq-1', 'obj-1', 'research', 0.5, 'tag_overlap')`, nil},
	} {
		if _, err := d.db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatalf("seed at v46: %v", err)
		}
	}
}

// assertOwnerPrincipalColumns checks that every per-principal table has
// exactly one owner_principal column and wantOwned rows owned by the
// instance owner.
func assertOwnerPrincipalColumns(t *testing.T, d *Driver, wantOwned int) {
	t.Helper()
	for _, table := range perPrincipalTables {
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'owner_principal'`, table); n != 1 {
			t.Errorf("%s.owner_principal columns: %d, want 1", table, n)
			continue
		}
		if n := scalarInt(t, d, `SELECT COUNT(*) FROM `+table+` WHERE owner_principal = ?`, storage.OwnerPrincipal); n != wantOwned {
			t.Errorf("%s rows owned by %q: %d, want %d", table, storage.OwnerPrincipal, n, wantOwned)
		}
	}
}

// assertLegacyRowsReadAsOwner checks that the stores read the rows seeded
// before 047 as the instance owner's.
func assertLegacyRowsReadAsOwner(t *testing.T, d *Driver) {
	t.Helper()
	ctx := context.Background()
	ss, err := d.SavedSearches().GetByName(ctx, "legacy")
	if err != nil || ss == nil {
		t.Fatalf("GetByName after 047 = %v, %v", ss, err)
	}
	if ss.OwnerPrincipal != storage.OwnerPrincipal {
		t.Errorf("legacy saved search owner = %q, want %q", ss.OwnerPrincipal, storage.OwnerPrincipal)
	}
	hist, err := d.SearchHistory().List(ctx, storage.SearchHistoryFilter{})
	if err != nil || len(hist) != 1 || hist[0].OwnerPrincipal != storage.OwnerPrincipal {
		t.Errorf("history after 047 = %+v, %v; want the legacy row owned by %q", hist, err, storage.OwnerPrincipal)
	}
	queue, err := d.Resurfacing().List(ctx, storage.ResurfacingFilter{})
	if err != nil || len(queue) != 1 || queue[0].OwnerPrincipal != storage.OwnerPrincipal {
		t.Errorf("queue after 047 = %+v, %v; want the legacy row owned by %q", queue, err, storage.OwnerPrincipal)
	}
}

// TestMigration047_OwnerPrincipal migrates a schema-46 database holding a
// saved search, a history entry and a queued candidate: every table gains
// owner_principal, the legacy rows belong to the instance owner and the
// stores read that back, a row written after the migration without a
// principal is the owner's too, and re-running 047 is a no-op.
func TestMigration047_OwnerPrincipal(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v46.db"), 46)
	t.Cleanup(func() { d.Close(ctx) })

	for _, table := range perPrincipalTables {
		if hasTableColumn(t, d, table, "owner_principal") {
			t.Fatalf("schema 46 already has %s.owner_principal: nothing for 047 to add", table)
		}
	}
	seedPerPrincipalRows(t, d)

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertOwnerPrincipalColumns(t, d, 1)
	assertLegacyRowsReadAsOwner(t, d)

	if err := d.SearchHistory().Append(ctx, &storage.SearchHistoryEntry{
		ID: "sh-2", Query: "after 047", SearchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("Append after 047: %v", err)
	}
	owned, err := d.SearchHistory().List(ctx, storage.SearchHistoryFilter{OwnerPrincipal: storage.OwnerPrincipal})
	if err != nil || len(owned) != 2 {
		t.Errorf("owner's history after a principal-less append = %d rows, %v; want 2", len(owned), err)
	}

	if err := migrate047OwnerPrincipal(ctx, d); err != nil {
		t.Fatalf("re-run 047: %v", err)
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM pragma_table_info('search_history') WHERE name = 'owner_principal'`); n != 1 {
		t.Errorf("search_history.owner_principal columns after re-run: %d, want 1", n)
	}
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
}

// TestMigration047_FreshInstall pins that a fresh database carries
// owner_principal on every per-principal table.
func TestMigration047_FreshInstall(t *testing.T) {
	d := newTestDriver(t)
	for _, table := range perPrincipalTables {
		if !hasTableColumn(t, d, table, "owner_principal") {
			t.Errorf("fresh install lacks %s.owner_principal", table)
		}
	}
}
