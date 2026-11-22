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

// TestPgMigration046_StewardAudit mirrors SQLite 065: an ordinary read
// logged at 45 keeps no grant and no link, the chain head starts empty,
// a steward entry chains from it, and a replay keeps the head where the
// append moved it.
func TestPgMigration046_StewardAudit(t *testing.T) {
	ctx := context.Background()
	dsn, _ := freshDatabaseDSN(t)
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t))
	if err != nil {
		t.Fatalf("postgres.New: %v", err)
	}
	t.Cleanup(func() { drv.Close(ctx) })
	if err := drv.MigrateThroughForTest(ctx, 45); err != nil {
		t.Fatalf("migrate through 45: %v", err)
	}
	db := drv.DB()
	if _, err := db.ExecContext(ctx, `INSERT INTO access_log
		(id, at, principal_id, route, policy_versions, object_count, entity_count, decision, checks, operation_digest)
		VALUES ('acc_old', NOW(), 'prn_old', 'GET /objects', 'v1', 0, 0, 'allowed', '[]', 'v1:sha256:00')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	log := drv.Sharing().AccessLog()
	old, err := log.Query(ctx, storage.AccessFilter{PrincipalID: "prn_old"})
	if err != nil || len(old) != 1 || old[0].StewardGrantID != "" || old[0].Chain != nil {
		t.Fatalf("old row after 46 = %+v, %v; want no grant and no link", old, err)
	}
	head, entries, err := log.StewardChain(ctx)
	if err != nil || head != (storage.AccessChainHead{}) || len(entries) != 0 {
		t.Fatalf("chain after 46 = %+v %v %v, want empty", head, entries, err)
	}
	e := &storage.AccessEntry{
		PrincipalID: "prn_steward", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		OperationDigest: storage.DigestPrefix + "ab", StewardGrantID: "stg_1", ObjectIDs: []string{"obj"}, ObjectCount: 1,
	}
	if err := log.AppendSteward(ctx, e); err != nil {
		t.Fatalf("AppendSteward after 46: %v", err)
	}
	assertPgStewardAuditConstraints(ctx, t, db)
	if err := drv.ReplayStatementsForTest(ctx, 46); err != nil {
		t.Fatalf("replay 46: %v", err)
	}
	head, entries, err = log.StewardChain(ctx)
	if err != nil || head.Seq != 1 || len(entries) != 1 {
		t.Fatalf("chain after replay = %+v, %d entries, %v; want the append kept", head, len(entries), err)
	}
	if err := storage.VerifyAccessChain(head, entries, time.Time{}); err != nil {
		t.Fatalf("chain after replay: %v", err)
	}
}

// assertPgStewardAuditConstraints checks access_log refuses a half-set
// chain link and access_log_chain keeps a single head row.
func assertPgStewardAuditConstraints(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO access_log_chain (id, seq, head) VALUES (2, 0, '')`); err == nil {
		t.Error("access_log_chain took a second head row")
	}
	for name, cols := range map[string]string{
		"a grant id and no chain position": `steward_grant_id`,
		"a chain position and no grant id": `chain_seq`,
	} {
		val := "'9'"
		if cols == "chain_seq" {
			val = "9"
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO access_log
			(id, at, principal_id, route, policy_versions, object_count, entity_count, decision, checks, operation_digest, `+cols+`)
			VALUES ('acc_half_`+cols+`', NOW(), 'prn_x', 'GET /objects', 'v1', 0, 0, 'allowed', '[]', 'v1:sha256:00', `+val+`)`); err == nil {
			t.Errorf("access_log took %s", name)
		}
	}
}
