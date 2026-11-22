package sqlite

import (
	"context"
	"fmt"
)

// stewardGrantDDL creates the steward grants table (migration 063,
// ADR-083 §2). Times are Unix milliseconds (UTC) like the approval
// tables; the scope is JSON in TEXT, typed by the kind that froze it,
// not by the schema. One grant per approved request (request_id is
// unique); a revoked grant carries its revocation and a grant that is
// not revoked carries none (the check). IF NOT EXISTS makes every
// statement re-runnable.
var stewardGrantDDL = []string{
	`CREATE TABLE IF NOT EXISTS steward_grants (
		id            TEXT PRIMARY KEY,
		request_id    TEXT NOT NULL UNIQUE REFERENCES approval_requests(id),
		steward_id    TEXT NOT NULL REFERENCES principals(id),
		matter_ref    TEXT NOT NULL DEFAULT '',
		scope         TEXT NOT NULL,
		scope_digest  TEXT NOT NULL,
		status        TEXT NOT NULL CHECK (status IN ('approved', 'revoked')),
		not_before    INTEGER NOT NULL,
		expires_at    INTEGER NOT NULL,
		approved_at   INTEGER NOT NULL,
		revoked_at    INTEGER,
		revoked_by    TEXT NOT NULL DEFAULT '',
		revoke_reason TEXT NOT NULL DEFAULT '',
		renews_id     TEXT REFERENCES steward_grants(id),
		CHECK (expires_at > not_before),
		CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
	)`,
	`CREATE INDEX IF NOT EXISTS idx_steward_grants_steward ON steward_grants(steward_id, status)`,
}

// migrate063StewardGrants creates steward_grants. New table only; a
// replay is a no-op.
func migrate063StewardGrants(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range stewardGrantDDL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create steward grants table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
