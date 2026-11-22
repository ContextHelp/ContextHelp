package sqlite

import (
	"context"
	"fmt"
)

// stewardAuditColumns are the columns migration 065 adds to access_log
// (ADR-083 §5): the steward grant an elevated request named, and its
// place in the tamper-evident chain. Every one is NULL on an ordinary
// entry, so rows logged before it need no backfill.
var stewardAuditColumns = []struct{ column, ddl string }{
	{"steward_grant_id", `TEXT`},
	{"chain_seq", `INTEGER`},
	{"prev_digest", `TEXT`},
	{"entry_digest", `TEXT`},
	{"objects_digest", `TEXT`},
}

// stewardAuditDDL indexes the steward entries (by grant, and one entry
// per chain position) and creates the chain head: one row, seeded
// empty. IF NOT EXISTS and INSERT OR IGNORE make every statement
// re-runnable.
var stewardAuditDDL = []string{
	`CREATE INDEX IF NOT EXISTS idx_access_log_steward ON access_log(steward_grant_id, at) WHERE steward_grant_id IS NOT NULL`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_access_log_chain_seq ON access_log(chain_seq) WHERE chain_seq IS NOT NULL`,
	`CREATE TABLE IF NOT EXISTS access_log_chain (
		id   INTEGER PRIMARY KEY CHECK (id = 1),
		seq  INTEGER NOT NULL CHECK (seq >= 0),
		head TEXT NOT NULL
	)`,
	`INSERT OR IGNORE INTO access_log_chain (id, seq, head) VALUES (1, 0, '')`,
}

// migrate065StewardAudit adds the steward audit columns and the chain
// head. Each column is added only when missing, so a replay is a
// no-op.
func migrate065StewardAudit(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range stewardAuditColumns {
		has, err := txTableHasColumn(ctx, tx, "access_log", c.column)
		if err != nil {
			return fmt.Errorf("inspect access_log.%s: %w", c.column, err)
		}
		if has {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE access_log ADD COLUMN `+c.column+` `+c.ddl); err != nil {
			return fmt.Errorf("add access_log.%s: %w", c.column, err)
		}
	}
	for _, stmt := range stewardAuditDDL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("steward audit chain: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
