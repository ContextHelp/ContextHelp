package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration065_StewardAudit migrates a schema-64 database holding
// an ordinary read: it keeps no grant and no link, the chain head
// starts empty, a steward entry chains from it, and a replay is a
// no-op that keeps the head where the append moved it.
func TestMigration065_StewardAudit(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v64.db"), 64)
	t.Cleanup(func() { d.Close(ctx) })

	for _, c := range stewardAuditColumns {
		if hasTableColumn(t, d, "access_log", c.column) {
			t.Fatalf("schema 64 already has access_log.%s: nothing for 065 to add", c.column)
		}
	}
	if hasTable(t, d, "access_log_chain") {
		t.Fatal("schema 64 already has access_log_chain")
	}
	if _, err := d.db.Exec(`INSERT INTO access_log
		(id, at, principal_id, route, policy_versions, object_count, entity_count, decision, checks, operation_digest)
		VALUES ('acc_old', 1760529600000, 'prn_old', 'GET /objects', 'v1', 0, 0, 'allowed', '[]', 'v1:sha256:00')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	old, err := d.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: "prn_old"})
	if err != nil || len(old) != 1 || old[0].StewardGrantID != "" || old[0].Chain != nil {
		t.Fatalf("old row after 065 = %+v, %v; want no grant and no link", old, err)
	}
	head, entries, err := d.Sharing().AccessLog().StewardChain(ctx)
	if err != nil || head != (storage.AccessChainHead{}) || len(entries) != 0 {
		t.Fatalf("chain after 065 = %+v %v %v, want empty", head, entries, err)
	}

	e := &storage.AccessEntry{
		PrincipalID: "prn_steward", Route: "GET /objects", Decision: storage.AccessDecisionAllowed,
		OperationDigest: storage.DigestPrefix + "ab", StewardGrantID: "stg_1", ObjectIDs: []string{"obj"}, ObjectCount: 1,
	}
	if err := d.Sharing().AccessLog().AppendSteward(ctx, e); err != nil {
		t.Fatalf("AppendSteward after 065: %v", err)
	}
	if _, err := d.db.Exec(`INSERT INTO access_log_chain (id, seq, head) VALUES (2, 0, '')`); err == nil {
		t.Error("access_log_chain took a second head row")
	}

	if err := migrate065StewardAudit(ctx, d); err != nil {
		t.Fatalf("re-run 065: %v", err)
	}
	head, entries, err = d.Sharing().AccessLog().StewardChain(ctx)
	if err != nil || head.Seq != 1 || len(entries) != 1 {
		t.Fatalf("chain after replay = %+v, %d entries, %v; want the append kept", head, len(entries), err)
	}
	if err := storage.VerifyAccessChain(head, entries, time.Time{}); err != nil {
		t.Fatalf("chain after replay: %v", err)
	}
}
