package sqlite

import (
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"strings"
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

//go:embed migrations/019_metering_events.sql
var migration019 string

//go:embed migrations/020_vec_objects.sql
var migration020 string

//go:embed migrations/021_saved_searches.sql
var migration021 string

//go:embed migrations/022_graph_canonical.sql
var migration022 string

//go:embed migrations/023_projected_fts_body.sql
var migration023 string

//go:embed migrations/024_rename_mention_uris.sql
var migration024 string

//go:embed migrations/025_federation_watermarks.sql
var migration025 string

//go:embed migrations/026_source_key.sql
var migration026 string

// Migrations 027 and 028 are now Go fns (migrate027JobsUserMentions /
// migrate028JobsUserHints) — see T-0186. The raw ALTER SQL was vulnerable
// to "duplicate column" errors when a test setup keeps one driver handle
// open while another driver Init runs against the same DB path. The Go
// fns check pragma_table_info before each ALTER, mirroring 014/022/031.

//go:embed migrations/029_index_signatures.sql
var migration029 string

//go:embed migrations/030_stamp_pipeline_version.sql
var migration030 string

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
	{Version: 19, SQL: migration019},
	// Migration 20: vec0 virtual table for ANN search via sqlite-vec.
	// Uses a Go fn so the {DIMENSION} placeholder is filled from d.vectorDimension.
	{Version: 20, fn: migrate020VecObjects},
	{Version: 21, SQL: migration021},
	// Migration 022: graph_json column on objects + object_nodes table.
	// Uses a Go fn so the backfill (graph_json = '{}' for legacy rows) runs
	// atomically after the DDL.
	{Version: 22, fn: migrate022GraphCanonical},
	// Migration 023: projected_fts_body column + FTS rebuild (T-0195).
	// Uses a Go fn for idempotency: checks for column before ALTER TABLE.
	{Version: 23, fn: migrate023ProjectedFTSBody},
	// Migration 024: rename mention_uris → mentions on objects table.
	// Uses a Go fn for idempotency: skips if column is already named 'mentions'.
	{Version: 24, fn: migrate024RenameMentionUris},
	// Migration 025: federation_watermarks table for tracking per-federation sync state.
	{Version: 25, SQL: migration025},
	// Migration 026: source_key column for external dedup key (Slack ts, tweet ID, etc.).
	{Version: 26, SQL: migration026},
	// Migration 027: user_mentions column on jobs for `ctxt analyze --mentions` (T-0190).
	// Idempotent Go fn (T-0186) — pragma_table_info check before ALTER.
	{Version: 27, fn: migrate027JobsUserMentions},
	// Migration 028: user_hints column on jobs for `ctxt capture --hint` (T-0573).
	// Idempotent Go fn (T-0186).
	{Version: 28, fn: migrate028JobsUserHints},
	// Migration 029: index_signatures table for ADR-070 bucket-1 (reindex_auto)
	// detection (T-0579). Detection-only; reindex worker lands in T-0581.
	{Version: 29, SQL: migration029},
	// Migration 030: stamp existing objects.pipeline rows with @v0 so post-T-0579
	// pipeline versioning has a baseline (ADR-070 §2). Idempotent.
	{Version: 30, SQL: migration030},
	// Migration 031: user_profile + user_note columns on jobs for
	// `ctxt capture --profile` and `--note` (T-0588). Idempotent Go fn
	// (mirrors migrate014RemindAt) so re-init on a partially-migrated
	// DB doesn't trip duplicate-column errors.
	{Version: 31, fn: migrate031JobsUserProfileNote},
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

// migrate022GraphCanonical adds graph_json to objects and creates the
// object_nodes denormalised index table. Idempotent: checks pragma_table_info
// before ALTER TABLE, uses IF NOT EXISTS for table/index DDL, and backfills
// graph_json = '{}' for legacy NULL rows.
func migrate022GraphCanonical(ctx context.Context, d *Driver) error {
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

	if !existing["graph_json"] {
		if _, err := d.db.ExecContext(ctx,
			`ALTER TABLE objects ADD COLUMN graph_json TEXT DEFAULT NULL`); err != nil {
			return err
		}
	}

	if _, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS object_nodes (
		    id          TEXT PRIMARY KEY,
		    object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
		    node_type   TEXT NOT NULL,
		    ordinal     INTEGER NOT NULL DEFAULT 0,
		    content     TEXT DEFAULT '',
		    created_at  TEXT NOT NULL
		)`); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_object_id ON object_nodes(object_id)`); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_node_type ON object_nodes(node_type)`); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_object_node_type ON object_nodes(object_id, node_type)`); err != nil {
		return err
	}

	_, err = d.db.ExecContext(ctx,
		`UPDATE objects SET graph_json = '{}' WHERE graph_json IS NULL`)
	return err
}

// migrate023ProjectedFTSBody adds projected_fts_body to objects and rebuilds the
// FTS virtual table to index it instead of raw summaries/raw_content (T-0195).
// Idempotent: checks pragma_table_info before ALTER TABLE.
func migrate023ProjectedFTSBody(ctx context.Context, d *Driver) error {
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

	if !existing["projected_fts_body"] {
		if _, err := d.db.ExecContext(ctx,
			`ALTER TABLE objects ADD COLUMN projected_fts_body TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}

	// Drop and recreate FTS to index projected_fts_body.
	if _, err := d.db.ExecContext(ctx, `DROP TABLE IF EXISTS objects_fts`); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `
		CREATE VIRTUAL TABLE objects_fts USING fts5(
		    id UNINDEXED,
		    projected_fts_body,
		    content='objects',
		    content_rowid='rowid'
		)`)
	return err
}

// migrate024RenameMentionUris renames the mention_uris column back to mentions.
// Idempotent: skips the rename if the column is already named 'mentions'.
func migrate024RenameMentionUris(ctx context.Context, d *Driver) error {
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
		if name == "mentions" {
			// Already renamed — nothing to do.
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = d.db.ExecContext(ctx, `ALTER TABLE objects RENAME COLUMN mention_uris TO mentions`)
	return err
}

// migrate020VecObjects creates the vec0 virtual table for sqlite-vec ANN search.
// The {DIMENSION} placeholder is replaced with d.vectorDimension so the table
// matches the embedding model in use.
func migrate020VecObjects(ctx context.Context, d *Driver) error {
	dim := d.vectorDimension
	if dim <= 0 {
		dim = DefaultVectorDimension
	}
	ddl := strings.ReplaceAll(migration020, "{DIMENSION}", strconv.Itoa(dim))
	_, err := d.db.ExecContext(ctx, ddl)
	return err
}

// addJobsColumnIfMissing adds a single TEXT column to the jobs table if
// it doesn't already exist (T-0186). The check uses pragma_table_info so
// re-running on a partially-migrated DB (e.g. test setups that double-open
// the same path) is a no-op rather than a duplicate-column error.
func addJobsColumnIfMissing(ctx context.Context, d *Driver, name, def string) error {
	existing := map[string]bool{}
	rows, err := d.db.QueryContext(ctx, "SELECT name FROM pragma_table_info('jobs')")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return err
		}
		existing[col] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if existing[name] {
		return nil
	}
	_, err = d.db.ExecContext(ctx, "ALTER TABLE jobs ADD COLUMN "+name+" "+def)
	return err
}

// migrate027JobsUserMentions adds user_mentions TEXT column to jobs
// (T-0190). Originally raw ALTER SQL; converted to idempotent Go fn in
// T-0186 to match the pattern of 014/022/031.
func migrate027JobsUserMentions(ctx context.Context, d *Driver) error {
	return addJobsColumnIfMissing(ctx, d, "user_mentions", "TEXT DEFAULT ''")
}

// migrate028JobsUserHints adds user_hints TEXT column to jobs (T-0573).
// Originally raw ALTER SQL; converted to idempotent Go fn in T-0186.
func migrate028JobsUserHints(ctx context.Context, d *Driver) error {
	return addJobsColumnIfMissing(ctx, d, "user_hints", "TEXT DEFAULT ''")
}

// migrate031JobsUserProfileNote adds user_profile + user_note TEXT columns to
// jobs (T-0588). Idempotent — checks pragma_table_info before each ALTER so
// repeated Init calls on a partially-migrated DB (e.g. tests that hold a
// driver handle open while a second handle reads pre-WAL-checkpoint state)
// don't trip "duplicate column name" errors.
func migrate031JobsUserProfileNote(ctx context.Context, d *Driver) error {
	if err := addJobsColumnIfMissing(ctx, d, "user_profile", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	return addJobsColumnIfMissing(ctx, d, "user_note", "TEXT DEFAULT ''")
}

