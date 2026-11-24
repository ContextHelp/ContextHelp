package sqlite

import (
	"context"
	"fmt"
)

// migrate068CaptureEncryption adds per-capture and per-watch encryption
// columns (objects.encrypted/encrypt_scope, jobs.user_encrypt/user_encrypt_scope,
// watches.encrypt/encrypt_scope). Idempotent per column.
func migrate068CaptureEncryption(ctx context.Context, d *Driver) error {
	if err := addTableColumnIfMissing(ctx, d, "objects", "encrypted", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add objects.encrypted: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "objects", "encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add objects.encrypt_scope: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_encrypt", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add jobs.user_encrypt: %w", err)
	}
	if err := addJobsColumnIfMissing(ctx, d, "user_encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add jobs.user_encrypt_scope: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "watches", "encrypt", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("add watches.encrypt: %w", err)
	}
	if err := addTableColumnIfMissing(ctx, d, "watches", "encrypt_scope", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add watches.encrypt_scope: %w", err)
	}
	return nil
}
