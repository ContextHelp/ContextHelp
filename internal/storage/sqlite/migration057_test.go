package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestMigration057_AccessReceipt migrates a schema-56 database holding
// admitted reads logged before receipts: they backfill as allowed with
// an empty trail, new entries store the receipt, and a replay is a
// no-op that keeps both.
func TestMigration057_AccessReceipt(t *testing.T) {
	ctx := context.Background()
	d := openAtVersion(t, filepath.Join(t.TempDir(), "v56.db"), 56)
	t.Cleanup(func() { d.Close(ctx) })

	for _, col := range []string{"decision", "checks", "operation_digest"} {
		if has, err := tableHasColumn(ctx, d, "access_log", col); err != nil || has {
			t.Fatalf("schema 56 access_log.%s present=%v err=%v: nothing for 057 to add", col, has, err)
		}
	}
	if _, err := d.db.Exec(`INSERT INTO access_log
		(id, at, principal_id, identity_id, route, policy_versions, object_count, entity_count)
		VALUES ('acc_old', 1760529600000, 'prn_old', NULL, 'GET /objects', 'v1', 2, 1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := d.migrateThrough(ctx, latestMigrationVersion()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	assertOldRow := func(stage string) {
		t.Helper()
		got, err := d.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: "prn_old"})
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: Query = %+v, %v", stage, got, err)
		}
		e := got[0]
		if e.Decision != storage.AccessDecisionAllowed || len(e.Checks) != 0 || e.Checks == nil || e.OperationDigest != "" || e.ObjectCount != 2 {
			t.Errorf("%s: backfilled row = %+v, want allowed with an empty trail and no digest", stage, e)
		}
	}
	assertOldRow("after 057")

	err := d.Sharing().AccessLog().Append(ctx, []*storage.AccessEntry{{
		PrincipalID: "prn_new", Route: "GET /objects", Decision: storage.AccessDecisionDenied,
		Checks:          []storage.AccessCheck{{Check: "scope", Outcome: storage.AccessOutcomeFail, Reason: "scope_missing"}},
		OperationDigest: storage.DigestPrefix + "ab",
	}})
	if err != nil {
		t.Fatalf("Append after 057: %v", err)
	}
	if _, err := d.db.Exec(`INSERT INTO access_log (id, at, principal_id, route, policy_versions, object_count, entity_count, decision)
		VALUES ('acc_bad', 1, 'prn_bad', 'GET /x', '', 0, 0, 'maybe')`); err == nil {
		t.Error("access_log accepted decision 'maybe'")
	}

	if err := migrate057AccessReceipt(ctx, d); err != nil {
		t.Fatalf("re-run 057: %v", err)
	}
	assertOldRow("after replay")
	got, err := d.Sharing().AccessLog().Query(ctx, storage.AccessFilter{PrincipalID: "prn_new", Decision: storage.AccessDecisionDenied})
	if err != nil || len(got) != 1 || got[0].Checks[0].Reason != "scope_missing" {
		t.Errorf("new entry after replay = %+v, %v", got, err)
	}
}
