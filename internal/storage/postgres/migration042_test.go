//go:build integration

package postgres_test

import (
	"context"
	"testing"
)

// TestPgMigration042_Approvals mirrors SQLite 061: the approval tables
// appear on an upgrade from 41 with their constraints, and a replay is
// a no-op.
func TestPgMigration042_Approvals(t *testing.T) {
	ctx := context.Background()
	drv := pgAt40(t)
	if err := drv.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	db := drv.DB()
	check := func(stage string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO approval_requests
			(id, kind, action, requested_by, payload, payload_digest, approvals_required, status, requested_at, expires_at)
			VALUES ('apr_bad', 'k', 'a', 'owner', '{}', 'd', 1, 'expired', NOW(), NOW() + INTERVAL '1 hour')`); err == nil {
			t.Errorf("%s: approval_requests accepted the computed state 'expired'", stage)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO approval_decisions
			(request_id, approver_id, decision, payload_digest, decided_at)
			VALUES ('apr_missing', 'owner', 'approve', 'd', NOW())`); err == nil {
			t.Errorf("%s: approval_decisions accepted an unknown request", stage)
		}
	}
	check("after 42")
	if err := drv.ReplayStatementsForTest(ctx, 42); err != nil {
		t.Fatalf("replay 42: %v", err)
	}
	check("after replay")
}
