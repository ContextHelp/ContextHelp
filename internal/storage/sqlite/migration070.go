package sqlite

import (
	"context"
	"fmt"
)

// rederiveDDL creates the tables the automatic re-derive job keeps:
// derivation_signatures holds the entries behind each signature (the
// signature is a hash), rederive_attempts the tries (ordering) and strikes (towards a skip) per
// object and target, rederive_skips the objects the job left as stored, keyed by
// the target signature so a later change makes them eligible again. Each
// is a new table; a replay is a no-op.
var rederiveDDL = []string{
	`CREATE TABLE IF NOT EXISTS derivation_signatures (
		sig         TEXT PRIMARY KEY,
		pipeline    TEXT NOT NULL,
		entries     TEXT NOT NULL,
		recorded_at TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS rederive_attempts (
		object_id  TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
		target_sig TEXT NOT NULL,
		tries      INTEGER NOT NULL DEFAULT 0,
		strikes    INTEGER NOT NULL DEFAULT 0,
		at         TEXT NOT NULL,
		PRIMARY KEY (object_id, target_sig)
	)`,
	`CREATE TABLE IF NOT EXISTS rederive_skips (
		object_id  TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
		target_sig TEXT NOT NULL,
		reason     TEXT NOT NULL,
		detail     TEXT NOT NULL DEFAULT '',
		at         TEXT NOT NULL,
		PRIMARY KEY (object_id, target_sig)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_rederive_skips_target ON rederive_skips(target_sig)`,
}

// migrate070Rederive creates derivation_signatures, rederive_attempts and rederive_skips.
func migrate070Rederive(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range rederiveDDL {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create re-derive tables: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
