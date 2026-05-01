package postgres

import (
	"context"
	"database/sql"
	"fmt"
)

func (d *Driver) Migrate(ctx context.Context) error {
	migrations := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS objects (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			subtype TEXT DEFAULT '',
			raw_content TEXT DEFAULT '',
			content_type TEXT DEFAULT '',
			metadata JSONB DEFAULT '{}',
			summaries JSONB DEFAULT '[]',
			sections JSONB DEFAULT '[]',
			tags JSONB DEFAULT '[]',
			mentions JSONB DEFAULT '[]',
			decisions JSONB DEFAULT '[]',
			tasks JSONB DEFAULT '[]',
			embedding vector(1536),
			pipeline TEXT DEFAULT '',
			source TEXT DEFAULT '',
			registry_influences JSONB DEFAULT '[]',
			plugins JSONB DEFAULT '{}',
			content_hash TEXT DEFAULT '',
			reinforcement_count INTEGER DEFAULT 0,
			last_reinforced_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			fts_indexed BOOLEAN DEFAULT FALSE,
			vector_indexed BOOLEAN DEFAULT FALSE,
			status TEXT NOT NULL DEFAULT 'active',
			inbox_note TEXT DEFAULT '',
			remind_at TIMESTAMP,
			reminded_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS entities (
			slug          TEXT PRIMARY KEY,
			title         TEXT DEFAULT '',
			description   TEXT DEFAULT '',
			namespace     TEXT DEFAULT '',
			aliases       JSONB DEFAULT '[]',
			metadata      JSONB DEFAULT '{}',
			content_status TEXT NOT NULL DEFAULT 'full',
			version_hash  TEXT NOT NULL DEFAULT '',
			registry_url  TEXT NOT NULL DEFAULT '',
			created_at    TIMESTAMP NOT NULL,
			updated_at    TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_content_status ON entities(content_status)`,
		`CREATE INDEX IF NOT EXISTS idx_entities_registry_url ON entities(registry_url)`,
		`CREATE TABLE IF NOT EXISTS edges (
			id TEXT PRIMARY KEY,
			from_type TEXT NOT NULL,
			from_id TEXT NOT NULL,
			to_type TEXT NOT NULL,
			to_id TEXT NOT NULL,
			edge_type TEXT NOT NULL,
			weight REAL DEFAULT 1.0,
			metadata JSONB DEFAULT '{}',
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			payload TEXT DEFAULT '',
			pipeline TEXT DEFAULT '',
			source TEXT DEFAULT '',
			result_id TEXT DEFAULT '',
			error TEXT DEFAULT '',
			retry_count INTEGER DEFAULT 0,
			max_retries INTEGER DEFAULT 3,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			started_at TIMESTAMP,
			completed_at TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS pipelines (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT DEFAULT '',
			steps JSONB DEFAULT '[]',
			is_built_in BOOLEAN DEFAULT FALSE,
			archived BOOLEAN DEFAULT FALSE,
			sandbox JSONB DEFAULT '{}',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS steps (
			name TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			path TEXT DEFAULT '',
			metadata JSONB DEFAULT '{}',
			installed_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS registry_cache (
			registry_url TEXT PRIMARY KEY,
			manifest JSONB DEFAULT '{}',
			last_fetched TIMESTAMP NOT NULL,
			etag TEXT DEFAULT '',
			auto_update BOOLEAN DEFAULT FALSE
		)`,
		`CREATE TABLE IF NOT EXISTS system_reminders (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT DEFAULT '',
			source TEXT DEFAULT '',
			action_url TEXT DEFAULT '',
			dismissed BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS feeds (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL UNIQUE,
			title TEXT DEFAULT '',
			description TEXT DEFAULT '',
			site_url TEXT DEFAULT '',
			format TEXT DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			sync_interval TEXT NOT NULL DEFAULT '1h',
			etag TEXT DEFAULT '',
			last_modified TEXT DEFAULT '',
			last_sync TIMESTAMP,
			error_count INTEGER NOT NULL DEFAULT 0,
			last_error TEXT DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS feed_items (
			id TEXT PRIMARY KEY,
			feed_id TEXT NOT NULL REFERENCES feeds(id),
			guid TEXT NOT NULL,
			link TEXT DEFAULT '',
			object_id TEXT DEFAULT '',
			ingested_at TIMESTAMP NOT NULL,
			UNIQUE (feed_id, guid)
		)`,
		`CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'processing',
			total INTEGER DEFAULT 0,
			processed INTEGER DEFAULT 0,
			failed INTEGER DEFAULT 0,
			errors JSONB DEFAULT '[]',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS detectors (
			id           TEXT PRIMARY KEY,
			kind         TEXT NOT NULL,
			name         TEXT NOT NULL,
			pipeline_name TEXT NOT NULL,
			pattern      TEXT NOT NULL DEFAULT '',
			priority     INTEGER NOT NULL DEFAULT 100,
			enabled      BOOLEAN NOT NULL DEFAULT TRUE,
			created_at   TIMESTAMP NOT NULL,
			updated_at   TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS object_proximity (
			object_a    TEXT NOT NULL,
			object_b    TEXT NOT NULL,
			score       REAL NOT NULL,
			semantic    REAL NOT NULL DEFAULT 0,
			temporal    REAL NOT NULL DEFAULT 0,
			entity      REAL NOT NULL DEFAULT 0,
			origin      REAL NOT NULL DEFAULT 0,
			behavioral  REAL NOT NULL DEFAULT 0,
			computed_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (object_a, object_b),
			CHECK (object_a < object_b)
		)`,
		`CREATE TABLE IF NOT EXISTS watches (
			id               TEXT PRIMARY KEY,
			path             TEXT NOT NULL UNIQUE,
			mode             TEXT NOT NULL DEFAULT 'generic',
			include_patterns JSONB DEFAULT '[]',
			exclude_patterns JSONB DEFAULT '[]',
			debounce_ms      INTEGER NOT NULL DEFAULT 500,
			status           TEXT NOT NULL DEFAULT 'active',
			pipeline_override TEXT DEFAULT '',
			last_error       TEXT DEFAULT '',
			created_at       TIMESTAMP NOT NULL,
			updated_at       TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS watch_file_records (
			watch_id     TEXT NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
			file_path    TEXT NOT NULL,
			object_id    TEXT DEFAULT '',
			content_hash TEXT DEFAULT '',
			last_seen    TIMESTAMP NOT NULL,
			PRIMARY KEY (watch_id, file_path)
		)`,
		`CREATE TABLE IF NOT EXISTS aliases (
			alias       TEXT NOT NULL,
			object_id   TEXT NOT NULL,
			scope       TEXT NOT NULL DEFAULT 'global',
			profile     TEXT NOT NULL DEFAULT '',
			created_at  TIMESTAMP NOT NULL,
			updated_at  TIMESTAMP NOT NULL,
			PRIMARY KEY (alias, scope, profile)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id          TEXT PRIMARY KEY,
			event_type  TEXT NOT NULL,
			object_id   TEXT NOT NULL DEFAULT '',
			actor       TEXT NOT NULL DEFAULT 'system',
			payload     JSONB NOT NULL DEFAULT '{}',
			created_at  TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_type, from_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_type, to_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(edge_type)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_pipelines_archived ON pipelines(archived)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_hash ON objects(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_embedding ON objects USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`,
		`CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds(status)`,
		`CREATE INDEX IF NOT EXISTS idx_feed_items_feed_id ON feed_items(feed_id)`,
		`CREATE INDEX IF NOT EXISTS idx_feed_items_feed_guid ON feed_items(feed_id, guid)`,
		`CREATE INDEX IF NOT EXISTS idx_batches_status ON batches(status)`,
		`CREATE INDEX IF NOT EXISTS idx_detectors_kind ON detectors(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_detectors_enabled ON detectors(enabled)`,
		`CREATE INDEX IF NOT EXISTS idx_detectors_priority ON detectors(priority)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_a_score ON object_proximity(object_a, score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_b_score ON object_proximity(object_b, score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_score ON object_proximity(score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_computed ON object_proximity(computed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_watches_status ON watches(status)`,
		`CREATE INDEX IF NOT EXISTS idx_wfr_watch_id ON watch_file_records(watch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aliases_object_id ON aliases(object_id)`,
		`CREATE INDEX IF NOT EXISTS idx_aliases_scope_profile ON aliases(scope, profile)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_object_id ON audit_log(object_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_event_type ON audit_log(event_type)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_log(created_at)`,
		`CREATE TABLE IF NOT EXISTS registry_entitlements (
			registry_name TEXT PRIMARY KEY,
			plan          TEXT NOT NULL,
			namespaces    TEXT NOT NULL,
			expires_at    TIMESTAMP,
			fetched_at    TIMESTAMP NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS federation_watermarks (
			federation_name TEXT PRIMARY KEY,
			last_synced_at  TIMESTAMPTZ NOT NULL DEFAULT 'epoch'
		)`,
	}

	for i, m := range migrations {
		if _, err := d.db.ExecContext(ctx, m); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}

	// Graph-canonical migration: add graph_json + object_nodes (idempotent).
	if err := migrateGraphCanonical(ctx, d.db); err != nil {
		return fmt.Errorf("graph canonical migration: %w", err)
	}
	return nil
}

// migrateGraphCanonical adds graph_json JSONB to objects, creates the
// object_nodes denormalised index table, and backfills graph_json = '{}'
// for legacy NULL rows. Idempotent via IF NOT EXISTS / information_schema check.
func migrateGraphCanonical(ctx context.Context, db *sql.DB) error {
	// Check if graph_json column already exists.
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'objects' AND column_name = 'graph_json'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check graph_json column: %w", err)
	}
	if !exists {
		if _, err := db.ExecContext(ctx,
			`ALTER TABLE objects ADD COLUMN graph_json JSONB DEFAULT NULL`); err != nil {
			return fmt.Errorf("add graph_json column: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS object_nodes (
			id          TEXT PRIMARY KEY,
			object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			node_type   TEXT NOT NULL,
			ordinal     INTEGER NOT NULL DEFAULT 0,
			content     TEXT DEFAULT '',
			created_at  TIMESTAMP NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create object_nodes table: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_object_id ON object_nodes(object_id)`); err != nil {
		return fmt.Errorf("create object_nodes object_id index: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_node_type ON object_nodes(node_type)`); err != nil {
		return fmt.Errorf("create object_nodes node_type index: %w", err)
	}
	if _, err := db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_object_nodes_object_node_type ON object_nodes(object_id, node_type)`); err != nil {
		return fmt.Errorf("create object_nodes compound index: %w", err)
	}

	// Backfill: set graph_json = '{}' for rows that predate this migration.
	if _, err := db.ExecContext(ctx,
		`UPDATE objects SET graph_json = '{}' WHERE graph_json IS NULL`); err != nil {
		return fmt.Errorf("backfill graph_json: %w", err)
	}
	return nil
}

// Store type declarations. Implementations are in separate files.
type ObjectStore struct{ db *sql.DB }
type EntityStore struct{ db *sql.DB }
type EdgeStore struct{ db *sql.DB }
type JobStore struct{ db *sql.DB }
type PipelineStore struct{ db *sql.DB }
type StepStore struct{ db *sql.DB }
type RegistryStore struct{ db *sql.DB }
type ReminderStore struct{ db *sql.DB }
type FeedStore struct{ db *sql.DB }
type FeedItemStore struct{ db *sql.DB }
type BatchStore struct{ db *sql.DB }
