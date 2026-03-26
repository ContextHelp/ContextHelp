package sqlite

import (
	"context"
	_ "embed"
	"fmt"
	"time"
)

//go:embed migrations/001_initial.sql
var migration001 string

//go:embed migrations/002_feeds_and_batches.sql
var migration002 string

//go:embed migrations/003_content_hash_reinforcement.sql
var migration003 string

//go:embed migrations/004_vector_search.sql
var migration004 string

//go:embed migrations/005_detectors.sql
var migration005 string

//go:embed migrations/006_object_proximity.sql
var migration006 string

//go:embed migrations/007_text_content.sql
var migration007 string

//go:embed migrations/008_watches.sql
var migration008 string

//go:embed migrations/009_inbox_status.sql
var migration009 string

//go:embed migrations/010_aliases.sql
var migration010 string

//go:embed migrations/011_audit_log.sql
var migration011 string

//go:embed migrations/012_mention_uris.sql
var migration012 string

//go:embed migrations/013_entity_thin_sync.sql
var migration013 string

//go:embed migrations/014_remind_at.sql
var migration014 string

//go:embed migrations/015_profile_scoped_objects.sql
var migration015 string

//go:embed migrations/016_attachments.sql
var migration016 string

//go:embed migrations/017_resurfacing_queue.sql
var migration017 string

//go:embed migrations/018_registry_entitlements.sql
var migration018 string

type migration struct {
	Version int
	SQL     string
	// fn is an optional Go-level migration for cases where SQL alone is
	// insufficient (e.g. idempotent ALTER TABLE on upgraded DBs).
	fn func(ctx context.Context, d *Driver) error
}

var migrations = []migration{
	{Version: 1, SQL: migration001},
	{Version: 2, SQL: migration002},
	{Version: 3, SQL: migration003},
	{Version: 4, SQL: migration004},
	{Version: 5, SQL: migration005},
	{Version: 6, SQL: migration006},
	{Version: 7, SQL: migration007},
	{Version: 8, SQL: migration008},
	{Version: 9, SQL: migration009},
	{Version: 10, SQL: migration010},
	{Version: 11, SQL: migration011},
	{Version: 12, SQL: migration012},
	// Migration 013 uses a Go fn because some databases (upgraded from an
	// earlier numbering scheme) already have version 13 recorded but with
	// different content (remind_at instead of entity_thin_sync columns).
	{Version: 13, fn: migrate013EntityThinSync},
	// Migration 014 uses a Go fn because some databases (upgraded from an
	// earlier numbering scheme) already have remind_at/reminded_at applied
	// as part of their old migration 013. The fn checks before altering.
	{Version: 14, fn: migrate014RemindAt},
	{Version: 15, SQL: migration015},
	{Version: 16, SQL: migration016},
	{Version: 17, SQL: migration017},
	{Version: 18, SQL: migration018},
}

// migrate013EntityThinSync adds content_status, version_hash, registry_url to entities,
// skipping any column that already exists (idempotent for upgraded DBs).
func migrate013EntityThinSync(ctx context.Context, d *Driver) error {
	existing := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, "SELECT name FROM pragma_table_info('entities')")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, col := range []struct{ name, def string }{
		{"content_status", "TEXT NOT NULL DEFAULT 'full'"},
		{"version_hash", "TEXT NOT NULL DEFAULT ''"},
		{"registry_url", "TEXT NOT NULL DEFAULT ''"},
	} {
		if existing[col.name] {
			continue
		}
		if _, err := d.db.ExecContext(ctx,
			"ALTER TABLE entities ADD COLUMN "+col.name+" "+col.def,
		); err != nil {
			return err
		}
	}

	if _, err := d.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_entities_content_status ON entities(content_status)`); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_entities_registry_url ON entities(registry_url)`)
	return err
}

// migrate014RemindAt adds remind_at / reminded_at columns and index to objects,
// skipping any column that already exists (idempotent).
func migrate014RemindAt(ctx context.Context, d *Driver) error {
	existing := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, "SELECT name FROM pragma_table_info('objects')")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		existing[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, col := range []struct{ name, def string }{
		{"remind_at", "TEXT DEFAULT NULL"},
		{"reminded_at", "TEXT DEFAULT NULL"},
	} {
		if existing[col.name] {
			continue
		}
		if _, err := d.db.ExecContext(ctx,
			"ALTER TABLE objects ADD COLUMN "+col.name+" "+col.def,
		); err != nil {
			return err
		}
	}

	_, err = d.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_objects_remind_at ON objects(remind_at)
		    WHERE remind_at IS NOT NULL`)
	return err
}

func (d *Driver) Migrate(ctx context.Context) error {
	// Ensure schema_version table exists.
	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	row := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	// Repair: some databases were upgraded from an earlier numbering scheme
	// where migration 013 was "remind_at" instead of "entity_thin_sync".
	// Those DBs have schema_version=13 but lack content_status/version_hash/
	// registry_url. Apply the thin-sync columns now if they are missing.
	if current >= 13 {
		if err := migrate013EntityThinSync(ctx, d); err != nil {
			return fmt.Errorf("repair entity thin sync columns: %w", err)
		}
	}

	for _, m := range migrations {
		if m.Version <= current {
			continue
		}
		if m.fn != nil {
			if err := m.fn(ctx, d); err != nil {
				return fmt.Errorf("apply migration %d: %w", m.Version, err)
			}
		} else if _, err := d.db.ExecContext(ctx, m.SQL); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.Version, err)
		}
		if _, err := d.db.ExecContext(ctx,
			"INSERT INTO schema_version (version, applied_at) VALUES (?, ?)",
			m.Version, time.Now().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
	}

	return nil
}
