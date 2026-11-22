package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

// TestMigration063_StewardGrants migrates a schema-62 database: the
// steward grants table appears with its constraints, and a replay is a
// no-op.
func TestMigration063_StewardGrants(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v62.db"), 62)
	t.Cleanup(func() { d.Close(ctx) })
	if hasTable(t, d, "steward_grants") {
		t.Fatal("schema 62 already has steward_grants: nothing for 063 to create")
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		if !hasTable(t, d, "steward_grants") {
			t.Fatalf("%s: table steward_grants missing", stage)
		}
		// Each insert is valid but for the one constraint under test;
		// the foreign keys would refuse them first, so they are off on
		// this one connection.
		conn, err := d.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = conn.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()
		insert := func(id, status string, notBefore, expires int, revokedAt string) error {
			_, err := conn.ExecContext(ctx, `INSERT INTO steward_grants
				(id, request_id, steward_id, scope, scope_digest, status, not_before, expires_at, approved_at, revoked_at)
				VALUES (?, ?, 'prn_x', '{}', 'd', ?, ?, ?, 1, `+revokedAt+`)`, id, "req_"+id, status, notBefore, expires)
			return err
		}
		if err := insert("stg_ok_"+stage, "approved", 1, 2, "NULL"); err != nil {
			t.Errorf("%s: a valid grant was refused: %v", stage, err)
		}
		for name, err := range map[string]error{
			"the computed state active":        insert("stg_a", "active", 1, 2, "NULL"),
			"the computed state expired":       insert("stg_e", "expired", 1, 2, "NULL"),
			"an empty window":                  insert("stg_w", "approved", 2, 2, "NULL"),
			"a revoked grant with no time":     insert("stg_r", "revoked", 1, 2, "NULL"),
			"an approved grant with a revoked": insert("stg_s", "approved", 1, 2, "1"),
		} {
			if err == nil {
				t.Errorf("%s: steward_grants accepted %s", stage, name)
			}
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO steward_grants
			(id, request_id, steward_id, scope, scope_digest, status, not_before, expires_at, approved_at)
			VALUES ('stg_dup', 'req_stg_ok_`+stage+`', 'prn_x', '{}', 'd', 'approved', 1, 2, 1)`); err == nil {
			t.Errorf("%s: steward_grants accepted a second grant for one request", stage)
		}
	}
	check("migrated")
	if err := migrate063StewardGrants(ctx, d); err != nil {
		t.Fatalf("re-run 063: %v", err)
	}
	check("replayed")
}
