package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// objectListIndexes are the indexes migration 050 creates.
var objectListIndexes = []string{"idx_objects_created_at", "idx_objects_updated_at"}

// hasObjectIndex reports whether sqlite_master lists index on objects.
func hasObjectIndex(t *testing.T, d *Driver, index string) bool {
	t.Helper()
	return scalarInt(t, d, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'objects' AND name = ?`, index) != 0
}

// objectQueryPlan is EXPLAIN QUERY PLAN's detail lines for query, joined.
func objectQueryPlan(t *testing.T, d *Driver, query string, args ...any) string {
	t.Helper()
	rows, err := d.db.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}
	return strings.Join(lines, "; ")
}

// assertObjectListIndexes checks that both indexes exist and that the
// planner reaches List's statements through the right index: the exact
// count of the default scope is a covering walk of idx_objects_status;
// a page ordered by created_at or updated_at streams from that column's
// index with no sort step, whatever the status scope or an unindexed
// filter; a selective filter (profile, a non-default status, a change
// cursor) keeps its own index.
func assertObjectListIndexes(t *testing.T, d *Driver) {
	t.Helper()
	for _, index := range objectListIndexes {
		if !hasObjectIndex(t, d, index) {
			t.Errorf("index %s missing", index)
		}
	}
	countSQL, _, args := objectListSQL(storage.ObjectFilter{Limit: 20}, nil)
	if plan := objectQueryPlan(t, d, countSQL, args...); !strings.Contains(plan, "COVERING INDEX idx_objects_status") {
		t.Errorf("default count plan %q, want the covering walk of idx_objects_status", plan)
	}
	after := int64(0)
	for _, tc := range []struct {
		name    string
		filter  storage.ObjectFilter
		index   string // the index the page walks
		streams bool   // no sort step: the index gives the order
	}{
		{"default page", storage.ObjectFilter{Limit: 20}, "idx_objects_created_at", true},
		{"updated_at page", storage.ObjectFilter{Sort: "updated_at", Limit: 20}, "idx_objects_updated_at", true},
		{"ascending page", storage.ObjectFilter{Dir: "asc", Limit: 20}, "idx_objects_created_at", true},
		{"second page", storage.ObjectFilter{Limit: 20, Offset: 20}, "idx_objects_created_at", true},
		{"every status", storage.ObjectFilter{Status: storage.ObjectStatusAll, Limit: 20}, "idx_objects_created_at", true},
		{"type filter", storage.ObjectFilter{Type: "note", Limit: 20}, "idx_objects_created_at", true},
		{"profile filter", storage.ObjectFilter{ProfileID: "p", Limit: 20}, "idx_objects_profile_id", false},
		{"inbox", storage.ObjectFilter{Status: "inbox", Limit: 20}, "idx_objects_status", false},
		{"change order", storage.ObjectFilter{ChangedAfter: &after, Limit: 100}, "idx_objects_change_seq", true},
	} {
		_, pageSQL, args := objectListSQL(tc.filter, nil)
		plan := objectQueryPlan(t, d, pageSQL, args...)
		if !strings.Contains(plan, "USING INDEX "+tc.index) {
			t.Errorf("%s: plan %q, want it to walk %s", tc.name, plan, tc.index)
		}
		if tc.streams && strings.Contains(plan, "TEMP B-TREE") {
			t.Errorf("%s: plan %q sorts, want it to stream from %s", tc.name, plan, tc.index)
		}
	}
}

// TestMigration050_ObjectListIndexes migrates a schema-48 database holding
// an object: the indexes appear and cover the row, the planner uses them,
// re-running 050 is a no-op and the ledger ends at the latest version.
func TestMigration050_ObjectListIndexes(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v48.db"), 48)
	t.Cleanup(func() { d.Close(ctx) })

	for _, index := range objectListIndexes {
		if hasObjectIndex(t, d, index) {
			t.Fatalf("schema 48 already has %s: nothing for 050 to create", index)
		}
	}
	// By hand: the store writes columns later migrations add (classes, 051).
	if _, err := d.db.ExecContext(ctx, `INSERT INTO objects (id, type, raw_content, status, created_at, updated_at)
		VALUES ('m050', 'note', 'before the index', ?, '2026-10-15T00:00:00Z', '2026-10-15T00:00:00Z')`, storage.ObjectStatusActive); err != nil {
		t.Fatalf("Create at 48: %v", err)
	}

	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertObjectListIndexes(t, d)
	objs, total, err := d.Objects().List(ctx, storage.ObjectFilter{Limit: 20})
	if err != nil || total != 1 || len(objs) != 1 || objs[0].ID != "m050" {
		t.Errorf("List after 050 = %d objects (total %d), %v; want the row written before it", len(objs), total, err)
	}

	if err := migrate050ObjectListIndexes(ctx, d); err != nil {
		t.Fatalf("re-run 050: %v", err)
	}
	assertObjectListIndexes(t, d)
	if n := scalarInt(t, d, `SELECT MAX(version) FROM schema_version`); n != latestMigrationVersion() {
		t.Errorf("schema_version after migrate = %d, want %d", n, latestMigrationVersion())
	}
	if n := scalarInt(t, d, `SELECT COUNT(*) FROM schema_version WHERE version = 50`); n != 1 {
		t.Errorf("ledger rows for 050: %d, want 1", n)
	}
}

// TestMigration050_FreshInstall pins that a fresh database carries the
// list indexes and plans List through them.
func TestMigration050_FreshInstall(t *testing.T) {
	assertObjectListIndexes(t, newTestDriver(t))
}
