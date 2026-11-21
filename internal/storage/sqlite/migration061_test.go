package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration061_Approvals migrates a schema-60 database: the approval
// tables appear with their constraints, and a replay is a no-op.
func TestMigration061_Approvals(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v60.db"), 60)
	t.Cleanup(func() { d.Close(ctx) })
	for _, table := range []string{"approval_requests", "approval_decisions"} {
		if hasTable(t, d, table) {
			t.Fatalf("schema 60 already has %s: nothing for 061 to create", table)
		}
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		for _, table := range []string{"approval_requests", "approval_decisions"} {
			if !hasTable(t, d, table) {
				t.Errorf("%s: table %s missing", stage, table)
			}
		}
		if _, err := d.db.ExecContext(ctx, `INSERT INTO approval_requests
			(id, kind, action, requested_by, payload, payload_digest, approvals_required, status, requested_at, expires_at)
			VALUES ('apr_bad', 'k', 'a', 'owner', '{}', 'd', 1, 'expired', 1, 2)`); err == nil {
			t.Errorf("%s: approval_requests accepted the computed state 'expired'", stage)
		}
		if _, err := d.db.ExecContext(ctx, `INSERT INTO approval_requests
			(id, kind, action, requested_by, payload, payload_digest, approvals_required, status, requested_at, expires_at)
			VALUES ('apr_bad', 'k', 'a', 'owner', '{}', 'd', 0, 'requested', 1, 2)`); err == nil {
			t.Errorf("%s: approval_requests accepted N = 0", stage)
		}
	}
	check("migrated")
	if err := migrate061Approvals(ctx, d); err != nil {
		t.Fatalf("re-run 061: %v", err)
	}
	check("replayed")
}
