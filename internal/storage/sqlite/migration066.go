package sqlite

import (
	"context"
	"fmt"
)

// governanceDDL creates the object_holds, erasure_tombstones, and
// bos_obligations tables (migration 066). Times are Unix milliseconds (UTC).
// object_holds intentionally omits ON DELETE CASCADE on object_id so released
// hold audit rows survive subsequent object deletion, while active holds block
// deletion inside storage write transactions.
var governanceDDL = []string{
	`CREATE TABLE IF NOT EXISTS object_holds (
		id             TEXT PRIMARY KEY,
		object_id      TEXT NOT NULL,
		source_key     TEXT NOT NULL DEFAULT '',
		hold_id        TEXT NOT NULL,
		source         TEXT NOT NULL,
		reason         TEXT NOT NULL DEFAULT '',
		placed_by      TEXT NOT NULL,
		placed_at      INTEGER NOT NULL,
		released_by    TEXT NOT NULL DEFAULT '',
		released_at    INTEGER,
		release_reason TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_object_holds_active_pair ON object_holds(object_id, hold_id) WHERE released_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_object_holds_active_object ON object_holds(object_id) WHERE released_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_object_holds_active_hold ON object_holds(hold_id) WHERE released_at IS NULL`,
	`CREATE INDEX IF NOT EXISTS idx_object_holds_active_source_key ON object_holds(source_key) WHERE released_at IS NULL AND source_key != ''`,
	`CREATE TABLE IF NOT EXISTS erasure_tombstones (
		source_key    TEXT PRIMARY KEY,
		source        TEXT NOT NULL,
		obligation_id TEXT NOT NULL DEFAULT '',
		erased_at     INTEGER NOT NULL,
		erased_by     TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS bos_obligations (
		id             TEXT PRIMARY KEY,
		kind           TEXT NOT NULL,
		request_sha256 TEXT NOT NULL,
		state          TEXT NOT NULL,
		status_json    TEXT NOT NULL,
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL
	)`,
}

func migrate066GovernanceHoldsAndObligations(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range governanceDDL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create governance tables: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
