package sqlite

import (
	"context"
	"fmt"
)

// migrate067ObjectExpiry adds objects.expires_at (with a partial index for the
// expiry sweeper) and jobs.user_expires_at so async captures propagate expiry
// through the pipeline. Idempotent per column.
func migrate067ObjectExpiry(ctx context.Context, d *Driver) error {
	if err := addTableColumnIfMissing(ctx, d, "objects", "expires_at", "TEXT DEFAULT NULL"); err != nil {
		return fmt.Errorf("add objects.expires_at: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "objects", "expiry_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add objects.expiry_scope: %w", err)
	}
	if _, err := d.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_objects_expires_at ON objects(expires_at)
		WHERE expires_at IS NOT NULL`); err != nil {
		return fmt.Errorf("create idx_objects_expires_at: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_expires_at", "TEXT DEFAULT NULL"); err != nil {
		return fmt.Errorf("add jobs.user_expires_at: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_expiry_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add jobs.user_expiry_scope: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "watches", "expiry_ttl", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add watches.expiry_ttl: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "watches", "expiry_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add watches.expiry_scope: %w", err)
	}
	return nil
}

func addTableColumnIfMissing(ctx context.Context, d *Driver, table, name, def string) error {
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	present := false
	for rows.Next() {
		var cid int
		var colName, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &colName, &colType, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan %s column: %w", table, err)
		}
		if colName == name {
			present = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s columns: %w", table, err)
	}
	if present {
		return nil
	}
	_, err = d.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, name, def))
	return err
}
