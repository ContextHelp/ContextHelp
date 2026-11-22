//go:build integration

package postgres_test

import (
	"context"
	"testing"

	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pgAt43 is a database migrated through 43, with its driver.
func pgAt43(t *testing.T) *pgdrv.Driver {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(context.Background(), 43); err != nil {
		t.Fatalf("migrate through 43: %v", err)
	}
	return drv
}

// TestPgMigration044_StewardGrants mirrors SQLite 063: the steward
// grants table appears on an upgrade from 43 with its constraints, and
// a replay is a no-op.
func TestPgMigration044_StewardGrants(t *testing.T) {
	ctx := context.Background()
	drv := pgAt43(t)
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	db := drv.DB()
	check := func(stage string) {
		t.Helper()
		// Each insert is valid but for the one constraint under test,
		// so the foreign keys are disabled for the session (replica
		// role skips FK triggers).
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SET session_replication_role = replica`); err != nil {
			t.Skipf("cannot disable foreign keys for the check: %v", err)
		}
		defer func() { _, _ = conn.ExecContext(ctx, `SET session_replication_role = DEFAULT`) }()
		insert := func(id, status string, notBefore, expires, revokedAt string) error {
			_, err := conn.ExecContext(ctx, `INSERT INTO steward_grants
				(id, request_id, steward_id, scope, scope_digest, status, not_before, expires_at, approved_at, revoked_at)
				VALUES ($1, $2, 'prn_x', '{}', 'd', $3, `+notBefore+`, `+expires+`, NOW(), `+revokedAt+`)`, id, "req_"+id+stage, status)
			return err
		}
		if err := insert("stg_ok", "approved", "NOW()", "NOW() + INTERVAL '1 hour'", "NULL"); err != nil {
			t.Errorf("%s: a valid grant was refused: %v", stage, err)
		}
		for name, err := range map[string]error{
			"the computed state active":        insert("stg_a", "active", "NOW()", "NOW() + INTERVAL '1 hour'", "NULL"),
			"the computed state expired":       insert("stg_e", "expired", "NOW()", "NOW() + INTERVAL '1 hour'", "NULL"),
			"an empty window":                  insert("stg_w", "approved", "NOW()", "NOW()", "NULL"),
			"a revoked grant with no time":     insert("stg_r", "revoked", "NOW()", "NOW() + INTERVAL '1 hour'", "NULL"),
			"an approved grant with a revoked": insert("stg_s", "approved", "NOW()", "NOW() + INTERVAL '1 hour'", "NOW()"),
		} {
			if err == nil {
				t.Errorf("%s: steward_grants accepted %s", stage, name)
			}
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO steward_grants
			(id, request_id, steward_id, scope, scope_digest, status, not_before, expires_at, approved_at)
			VALUES ('stg_dup', 'req_stg_ok`+stage+`', 'prn_x', '{}', 'd', 'approved', NOW(), NOW() + INTERVAL '1 hour', NOW())`); err == nil {
			t.Errorf("%s: steward_grants accepted a second grant for one request", stage)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM steward_grants`); err != nil {
			t.Fatal(err)
		}
	}
	check("after 44")
	if err := drv.ReplayStatementsForTest(ctx, 44); err != nil {
		t.Fatalf("replay 44: %v", err)
	}
	check("after replay")
}
