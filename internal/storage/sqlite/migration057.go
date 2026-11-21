package sqlite

import (
	"context"
	"fmt"
)

// accessReceiptColumns are the receipt columns migration 057 adds to
// access_log (ADR-080 §9). Rows logged before it were all admitted
// reads, so they backfill as allowed with an empty trail and no digest.
var accessReceiptColumns = []struct{ column, ddl string }{
	{"decision", `TEXT NOT NULL DEFAULT 'allowed' CHECK (decision IN ('allowed', 'denied', 'deferred'))`},
	{"checks", `TEXT NOT NULL DEFAULT '[]'`},
	{"operation_digest", `TEXT NOT NULL DEFAULT ''`},
}

// migrate057AccessReceipt makes access_log receipt-shaped: the decision,
// the ordered check trail (a JSON array) and the operation digest, plus
// an index to list refusals. Each column is added only when missing and
// the index is IF NOT EXISTS, so a replay is a no-op.
func migrate057AccessReceipt(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range accessReceiptColumns {
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
	if _, err := tx.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_access_log_decision ON access_log(decision, at)`); err != nil {
		return fmt.Errorf("index access_log.decision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
