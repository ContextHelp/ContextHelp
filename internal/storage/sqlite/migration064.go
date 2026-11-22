package sqlite

import (
	"context"
	"fmt"
)

// uiSessionElevationColumns are the columns migration 064 adds to
// ui_sessions (ADR-083 §3): the steward grant a browser session is
// elevated into and the last request of the elevation (Unix
// milliseconds, UTC, like the other session times). A session stored
// before them is not elevated, so both stay NULL.
var uiSessionElevationColumns = []struct{ column, ddl string }{
	{"steward_grant_id", `TEXT`},
	{"steward_active_at", `INTEGER`},
}

// migrate064UISessionElevation adds the elevation columns to
// ui_sessions. Each column is added only when missing, so a replay is a
// no-op; nothing is an FTS input, so no re-projection follows.
func migrate064UISessionElevation(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, c := range uiSessionElevationColumns {
		has, err := txTableHasColumn(ctx, tx, "ui_sessions", c.column)
		if err != nil {
			return fmt.Errorf("inspect ui_sessions.%s: %w", c.column, err)
		}
		if has {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE ui_sessions ADD COLUMN `+c.column+` `+c.ddl); err != nil {
			return fmt.Errorf("add ui_sessions.%s: %w", c.column, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
