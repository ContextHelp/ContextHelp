package sqlite

import (
	"context"
	"fmt"
)

// approvalDDL creates the approval mechanism's tables (migration 061,
// ADR-083 §9). Times are Unix milliseconds (UTC) like the principal
// registry; the payload and preconditions are JSON in TEXT, typed by
// the kind that filed them, not by the schema. IF NOT EXISTS makes
// every statement re-runnable.
var approvalDDL = []string{
	`CREATE TABLE IF NOT EXISTS approval_requests (
		id                 TEXT PRIMARY KEY,
		kind               TEXT NOT NULL,
		action             TEXT NOT NULL,
		requested_by       TEXT NOT NULL REFERENCES principals(id),
		subject_id         TEXT REFERENCES principals(id),
		reason             TEXT NOT NULL DEFAULT '',
		payload            TEXT NOT NULL,
		payload_digest     TEXT NOT NULL,
		preconditions      TEXT NOT NULL DEFAULT '{}',
		approvals_required INTEGER NOT NULL CHECK (approvals_required >= 1),
		status             TEXT NOT NULL CHECK (status IN ('requested', 'approved', 'rejected', 'withdrawn', 'stale')),
		requested_at       INTEGER NOT NULL,
		expires_at         INTEGER NOT NULL,
		decided_at         INTEGER,
		result_ref         TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE INDEX IF NOT EXISTS idx_approval_requests_kind_status ON approval_requests(kind, status, expires_at)`,
	`CREATE TABLE IF NOT EXISTS approval_decisions (
		request_id     TEXT NOT NULL REFERENCES approval_requests(id),
		approver_id    TEXT NOT NULL REFERENCES principals(id),
		decision       TEXT NOT NULL CHECK (decision IN ('approve', 'reject')),
		payload_digest TEXT NOT NULL,
		note           TEXT NOT NULL DEFAULT '',
		decided_at     INTEGER NOT NULL,
		PRIMARY KEY (request_id, approver_id)
	)`,
}

// migrate061Approvals creates approval_requests and approval_decisions.
// New tables only; a replay is a no-op.
func migrate061Approvals(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range approvalDDL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create approval tables: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
