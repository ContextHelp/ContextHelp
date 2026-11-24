package sqlite

import (
	"context"
	"fmt"
)

// migrate068CaptureEncryption adds per-capture and per-watch encryption
// columns (objects.encrypted/encrypt_scope, jobs.user_encrypt/user_encrypt_scope,
// watches.encrypt/encrypt_scope). Idempotent per column.
func migrate068CaptureEncryption(ctx context.Context, d *Driver) error {
	if err := addTableColIfMissing(ctx, d, "objects", "encrypted", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add objects.encrypted: %w", err)
	}
	if err := addTableColIfMissing(ctx, d, "objects", "encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add objects.encrypt_scope: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_encrypt", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add jobs.user_encrypt: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add jobs.user_encrypt_scope: %w", err)
	}
	if err := addTableColIfMissing(ctx, d, "watches", "encrypt", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add watches.encrypt: %w", err)
	}
	if err := addTableColIfMissing(ctx, d, "watches", "encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add watches.encrypt_scope: %w", err)
	}
	return nil
}

func addTableColIfMissing(ctx context.Context, d *Driver, table, name, def string) error {
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
