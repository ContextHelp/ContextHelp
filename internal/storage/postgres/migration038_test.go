//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// pgAt37 is a database migrated through 37, with its driver.
func pgAt37(t *testing.T) (*pgdrv.Driver, string) {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.MigrateThroughForTest(context.Background(), 37); err != nil {
		t.Fatalf("migrate through 37: %v", err)
	}
	return drv, dsn
}

// TestPgMigration038_AccessReceipt mirrors SQLite 057: an access_log row
// written before receipts backfills as allowed with an empty trail, new
// entries store the receipt, the decision is constrained, and a replay
// is a no-op.
func TestPgMigration038_AccessReceipt(t *testing.T) {
	ctx := context.Background()
	drv, _ := pgAt37(t)
	db := drv.DB()
	if _, err := db.Exec(`INSERT INTO access_log
		(id, at, principal_id, identity_id, route, policy_versions, object_count, entity_count)
		VALUES ('acc_old', NOW(), 'prn_old', NULL, 'GET /objects', 'v1', 2, 1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	check := func(stage string) {
		t.Helper()
		got, err := drv.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: "prn_old"})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: Query = %+v, %v", stage, got, err)
		}
		e := got[0]
		if e.Decision != storage.AccessDecisionAllowed || e.Checks == nil || len(e.Checks) != 0 || e.OperationDigest != "" {
			t.Errorf("%s: backfilled row = %+v, want allowed with an empty trail and no digest", stage, e)
		}
	}
	check("after 38")

	if _, err := db.Exec(`INSERT INTO access_log (id, at, principal_id, route, policy_versions, object_count, entity_count, decision)
		VALUES ('acc_bad', NOW(), 'prn_bad', 'GET /x', '', 0, 0, 'maybe')`); err == nil {
		t.Error("access_log accepted decision 'maybe'")
	}
	if err := drv.ReplayAccessReceiptForTest(ctx); err != nil {
		t.Fatalf("replay 38: %v", err)
	}
	check("after replay")
}
