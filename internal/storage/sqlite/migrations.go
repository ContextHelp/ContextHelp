package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/rowscan"
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

//go:embed migrations/025_federation_watermarks.sql
var migration025 string

//go:embed migrations/026_source_key.sql
var migration026 string

// Migrations 027 and 028 are now Go fns (migrate027JobsUserMentions /
// migrate028JobsUserHints). The raw ALTER SQL was vulnerable
// to "duplicate column" errors when a test setup keeps one driver handle
// open while another driver Init runs against the same DB path. The Go
// fns check pragma_table_info before each ALTER, mirroring 014/022/031.

//go:embed migrations/029_index_signatures.sql
var migration029 string

//go:embed migrations/030_stamp_pipeline_version.sql
var migration030 string

//go:embed migrations/032_embedding_models.sql
var migration032 string

//go:embed migrations/034_vec_objects_cosine.sql
var migration034 string

//go:embed migrations/041_ui_sessions.sql
var migration041 string

//go:embed migrations/044_registry_pending_entities.sql
var migration044 string

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
	// Uses a Go fn so the {DIMENSION} placeholder is filled with
	// legacyVectorDimension. Dropped by 038.
	{Version: 20, fn: migrate020VecObjects},
	{Version: 21, SQL: migration021},
	// Migration 022: graph_json column on objects + object_nodes table.
	// Uses a Go fn so the backfill (graph_json = '{}' for legacy rows) runs
	// atomically after the DDL.
	{Version: 22, fn: migrate022GraphCanonical},
	// Migration 023: projected_fts_body column + FTS rebuild.
	// Uses a Go fn for idempotency: checks for column before ALTER TABLE.
	{Version: 23, fn: migrate023ProjectedFTSBody},
	// Migration 024: rename mention_uris → mentions on objects table.
	// Uses a Go fn for idempotency: skips if column is already named 'mentions'.
	{Version: 24, fn: migrate024RenameMentionUris},
	// Migration 025: federation_watermarks table for tracking per-federation sync state.
	{Version: 25, SQL: migration025},
	// Migration 026: source_key column for external dedup key (Slack ts, tweet ID, etc.).
	{Version: 26, SQL: migration026},
	// Migration 027: user_mentions column on jobs for `ctxt analyze --mentions`.
	// Idempotent Go fn — pragma_table_info check before ALTER.
	{Version: 27, fn: migrate027JobsUserMentions},
	// Migration 028: user_hints column on jobs for `ctxt capture --hint`.
	// Idempotent Go fn.
	{Version: 28, fn: migrate028JobsUserHints},
	// Migration 029: index_signatures table for ADR-070 bucket-1 (reindex_auto)
	// detection. Detection-only; reindex worker lives elsewhere.
	{Version: 29, SQL: migration029},
	// Migration 030: stamp existing objects.pipeline rows with @v0 so
	// pipeline versioning has a baseline (ADR-070 §2). Idempotent.
	{Version: 30, SQL: migration030},
	// Migration 031: user_profile + user_note columns on jobs for
	// `ctxt capture --profile` and `--note`. Idempotent Go fn
	// (mirrors migrate014RemindAt) so re-init on a partially-migrated
	// DB doesn't trip duplicate-column errors.
	{Version: 31, fn: migrate031JobsUserProfileNote},
	// Migration 032: embedding_models registry + embeddings composite-key
	// table for ADR-071 Phase 1. Plain SQL — DDL is fresh; the
	// CREATE TABLE IF NOT EXISTS / CREATE UNIQUE INDEX IF NOT EXISTS
	// guards make it safe to re-run.
	{Version: 32, SQL: migration032},
	// Migration 033: backfill legacy object_embeddings rows into the new
	// composite-key embeddings table under a synthetic model_id derived
	// from legacyVectorDimension. Also seeds the
	// matching embedding_models row (is_default = 1) and stamps the
	// `embeddings_<model_id>` row in index_signatures (ADR-070
	// integration). Idempotent — uses INSERT OR IGNORE on the composite
	// key and ON CONFLICT upserts on the singleton model row.
	{Version: 33, fn: migrate033EmbeddingsBackfill},
	// Migration 034: recreate vec_objects with distance_metric=cosine —
	// the pinned cross-driver distance contract. Index-only data; the fn
	// rebuilds rows from object_embeddings after the DDL swap, then
	// re-stamps embedding index signatures from the new DDL (033 stamps
	// from the pre-cosine table).
	{Version: 34, fn: migrate034VecObjectsCosine},
	// Migration 035: re-stamp embedding index signatures for DBs that
	// applied 034 before it re-stamped — their stored provenance still
	// hashes the dropped L2 table. Idempotent recomputation from the live
	// index.
	{Version: 35, fn: migrate035RestampEmbeddingSignatures},
	// Migration 036: idempotency_key column on jobs + partial unique
	// index, so a replayed enqueue (response lost in transit) resolves to
	// the existing job instead of minting a duplicate. Idempotent Go fn
	// (pragma_table_info check before ALTER; IF NOT EXISTS on the index).
	{Version: 36, fn: migrate036JobsIdempotencyKey},
	// Migration 037: per-model embeddings (ADR-071 amendment 2026-09-26).
	// Rebuilds embeddings with a stable rowid alias and an object FK,
	// deletes the legacy-blob placeholder, and builds one vec0 index per
	// registered model. Migrate re-runs the per-model pass on every open.
	{Version: 37, fn: migrate037PerModelEmbeddings},
	// Migration 038: drop the single-vector path (ADR-071 amendment
	// 2026-09-26): vec_objects, object_embeddings, objects.embeddings, and
	// objects.vector_indexed. Vectors live only in embeddings.
	{Version: 38, fn: migrate038DropSingleVectorPath},
	// Migration 039: claim_token + lease_expires_at on jobs. A worker
	// running a task job holds a renewed lease; stale recovery leaves a
	// leased job alone until the lease runs out, so a second process
	// cannot requeue and re-run a job another process is running.
	{Version: 39, fn: migrate039JobsClaimLease},
	// Migration 040: projection_version stamp on objects. Rows written
	// before it carry no stamp, so the fn also clears the stored FTS
	// signature: the next start reports a mismatch and schedules the
	// re-projection job, which stamps rows and signature again.
	{Version: 40, fn: migrate040ProjectionVersion},
	// Migration 041: ui_sessions + ui_login_codes for web UI browser
	// sign-in. New tables only; IF NOT EXISTS guards make it re-runnable.
	{Version: 41, SQL: migration041},
	// Migration 042: federation watermarks become change positions —
	// last_synced_at holds the updated_at and the new last_object_id the
	// id of the last object a target acknowledged — and objects get an
	// index in that change order, so a push lists only what changed.
	{Version: 42, fn: migrate042FederationChangeCursor},
	// Migration 043: objects.change_seq, a change position the store
	// stamps from a counter on every insert and update, backfilled in
	// (updated_at, id) order; federation watermarks become the last
	// acknowledged change_seq. updated_at is caller-set (imports and
	// federated rows keep their origin's), so it cannot order changes.
	{Version: 43, fn: migrate043ObjectChangeSeq},
	// Migration 044: registry_pending_entities, the approval queue for
	// entities untrusted registries serve. New table only; IF NOT
	// EXISTS makes it re-runnable.
	{Version: 44, SQL: migration044},
	// Migration 045: drop objects.plugins. There is no plugin system
	// (ADR-078) and no code reads or writes the column. Not part of the
	// FTS input, so no re-projection follows.
	{Version: 45, fn: migrate045DropObjectsPlugins},
	// Migration 046: object_proximity endpoints reference objects with
	// ON DELETE CASCADE, so deleting an object drops its proximity pairs
	// like its aliases and object_nodes. Rebuilds the table (SQLite
	// cannot add a foreign key in place) and drops pairs whose object is
	// already gone.
	{Version: 46, fn: migrate046ProximityObjectFK},
	// Migration 047: owner_principal on the per-principal tables
	// (search_history, saved_searches, resurfacing_queue), defaulting to
	// the instance owner so every existing row stays the owner's. Column
	// add only; nothing is an FTS input, so no re-projection follows.
	{Version: 47, fn: migrate047OwnerPrincipal},
	// Migration 048: the principal registry (ADR-079): principals,
	// principal_identities, auth_tickets and the kv table for API key
	// records, seeded with the one owner principal. New tables only,
	// nothing is an FTS input, so no re-projection follows.
	{Version: 48, fn: migrate048AuthRegistry},
	// Migration 049: ui_sessions and ui_login_codes reference the
	// identity that minted them (ADR-079) instead of the hash of a
	// static token. Rebuilds both tables (SQLite cannot add a foreign
	// key in place); the rows, minted by static tokens that no longer
	// authenticate, are dropped. Nothing is an FTS input, so no
	// re-projection follows.
	{Version: 49, fn: migrate049SessionsBoundToIdentities},
	// Migration 050: idx_objects_created_at and idx_objects_updated_at,
	// the columns ObjectStore.List orders by, so a page streams from its
	// sort column's index and stops at the page instead of fetching every
	// active object through idx_objects_status and sorting it. Indexes
	// only: no column changes, nothing is an FTS input, so no
	// re-projection follows. IF NOT EXISTS makes it re-runnable.
	{Version: 50, fn: migrate050ObjectListIndexes},
	// Migration 051: registry_entitlements.kind (outbound | inbound),
	// keyed by (kind, registry_name), so a remote registry's synced
	// entitlement and an operator's grant to a principal of the same
	// name are separate rows. Rebuilds the table (SQLite cannot change
	// a primary key in place) and backfills the kind of existing rows.
	{Version: 51, fn: migrate051EntitlementKind},
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
	if err := d.migrateThrough(ctx, migrations[len(migrations)-1].Version); err != nil {
		return err
	}
	// ADR-070 verify-and-rebuild for every registered model's vector index,
	// on every open: a signature that drifted since the last open (new
	// index DDL, registry dimension change, a dropped table) converges here.
	if err := ensureEmbeddingIndexes(ctx, d); err != nil {
		return fmt.Errorf("ensure embedding indexes: %w", err)
	}
	return nil
}

// migrateThrough applies every pending migration up to and including
// target. Migrate passes the latest version; tests pass an older one to
// build a database at a historic schema.
func (d *Driver) migrateThrough(ctx context.Context, target int) error {
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

	// Snapshot before anything below writes: a failed or wrong migration
	// is then recoverable from the copy. No snapshot, no migration.
	if err := d.snapshotBeforeMigrate(ctx, current, target); err != nil {
		return err
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
		if m.Version <= current || m.Version > target {
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
// FTS virtual table to index it instead of raw summaries/raw_content.
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

// migrate034VecObjectsCosine drops and recreates the vec0 virtual table
// with an explicit cosine distance metric, then rebuilds it from the
// canonical object_embeddings store. Only rows whose stored dimension
// matches the configured ANN dimension are indexed — the same
// mirror-on-write rule ObjectStore.Create applies.
func migrate034VecObjectsCosine(ctx context.Context, d *Driver) error {
	dim := legacyVectorDimension
	if _, err := d.db.ExecContext(ctx, `DROP TABLE IF EXISTS vec_objects`); err != nil {
		return fmt.Errorf("drop L2 vec_objects: %w", err)
	}
	ddl := strings.ReplaceAll(migration034, "{DIMENSION}", strconv.Itoa(dim))
	if _, err := d.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("recreate vec_objects with cosine metric: %w", err)
	}
	// object_embeddings stores raw little-endian float32 blobs — the same
	// encoding vec0 accepts — so the rebuild is a straight copy.
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO vec_objects(id, embedding)
		SELECT id, embedding FROM object_embeddings WHERE dimensions = ?`, dim,
	); err != nil {
		return fmt.Errorf("rebuild vec_objects from object_embeddings: %w", err)
	}
	// The DDL swap invalidates any embedding signature stamped from the old
	// L2 table (033 stamps before this migration runs). Re-stamp from the
	// live cosine index so a fresh install never carries provenance for a
	// dropped index.
	return restampEmbeddingSignatures(ctx, d)
}

// restampEmbeddingSignatures recomputes every stored embeddings_<model_id>
// index signature from the live vec_objects description. Migrations that
// change the ANN index shape (034's L2 → cosine swap) call this so stored
// provenance keeps describing the index that actually exists; otherwise the
// next ADR-070 verify pass reports drift the migration itself created. Only
// models that already carry a signature row are re-stamped — absent rows
// keep their first-boot semantics.
func restampEmbeddingSignatures(ctx context.Context, d *Driver) error {
	idx, err := indexsig.SQLiteVectorIndex(ctx, d.db)
	if err != nil {
		return fmt.Errorf("describe vector index for re-stamp: %w", err)
	}

	type model struct {
		id       string
		provider string
		dim      int
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT model_id, provider, dimension FROM embedding_models`)
	if err != nil {
		return fmt.Errorf("list embedding models for re-stamp: %w", err)
	}
	defer rows.Close()
	models, err := rowscan.All(rows, func(rows *sql.Rows) (model, error) {
		var m model
		err := rows.Scan(&m.id, &m.provider, &m.dim)
		return m, err
	})
	if err != nil {
		return err
	}

	for _, m := range models {
		sigID := EmbeddingSignatureID(m.id)
		stored, err := LoadIndexSignature(ctx, d.db, sigID)
		if err != nil {
			return fmt.Errorf("load signature %s: %w", sigID, err)
		}
		if stored == nil {
			continue
		}
		hash, summary := indexsig.ComputeEmbedding(
			m.id, m.provider, m.dim, idx.Method, idx.OpsClass, idx.BuildParams)
		if err := UpsertIndexSignature(ctx, d.db, sigID, hash, summary); err != nil {
			return fmt.Errorf("re-stamp %s: %w", sigID, err)
		}
	}
	return nil
}

// migrate035RestampEmbeddingSignatures converges DBs that applied 034
// before it re-stamped: their stored embedding signatures still hash the
// dropped L2 table's DDL. Recomputing from the live cosine index brings
// upgraded installs in line with fresh ones. Idempotent — re-running on an
// already-correct DB rewrites identical values.
func migrate035RestampEmbeddingSignatures(ctx context.Context, d *Driver) error {
	return restampEmbeddingSignatures(ctx, d)
}

// migrate020VecObjects creates the vec0 virtual table for sqlite-vec ANN search.
// The {DIMENSION} placeholder is replaced with legacyVectorDimension.
func migrate020VecObjects(ctx context.Context, d *Driver) error {
	ddl := strings.ReplaceAll(migration020, "{DIMENSION}", strconv.Itoa(legacyVectorDimension))
	_, err := d.db.ExecContext(ctx, ddl)
	return err
}

// addJobsColumnIfMissing adds a single TEXT column to the jobs table if
// it doesn't already exist. The check uses pragma_table_info so
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

// migrate027JobsUserMentions adds user_mentions TEXT column to jobs.
// Originally raw ALTER SQL; converted to idempotent Go fn in
// to match the pattern of 014/022/031.
func migrate027JobsUserMentions(ctx context.Context, d *Driver) error {
	return addJobsColumnIfMissing(ctx, d, "user_mentions", "TEXT DEFAULT ''")
}

// migrate028JobsUserHints adds user_hints TEXT column to jobs.
// Originally raw ALTER SQL; converted to idempotent Go fn.
func migrate028JobsUserHints(ctx context.Context, d *Driver) error {
	return addJobsColumnIfMissing(ctx, d, "user_hints", "TEXT DEFAULT ''")
}

// migrate031JobsUserProfileNote adds user_profile + user_note TEXT columns to
// jobs. Idempotent — checks pragma_table_info before each ALTER so
// repeated Init calls on a partially-migrated DB (e.g. tests that hold a
// driver handle open while a second handle reads pre-WAL-checkpoint state)
// don't trip "duplicate column name" errors.
func migrate031JobsUserProfileNote(ctx context.Context, d *Driver) error {
	if err := addJobsColumnIfMissing(ctx, d, "user_profile", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	return addJobsColumnIfMissing(ctx, d, "user_note", "TEXT DEFAULT ''")
}

// migrate036JobsIdempotencyKey adds the idempotency_key column plus a partial
// unique index over non-empty keys. The index is the race-window guard behind
// the lookup-then-insert dedupe at the enqueue surface: two concurrent
// submissions with the same key cannot both insert. Empty keys (every legacy
// row and every keyless enqueue) are exempt.
func migrate036JobsIdempotencyKey(ctx context.Context, d *Driver) error {
	if err := addJobsColumnIfMissing(ctx, d, "idempotency_key", "TEXT DEFAULT ''"); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS
		idx_jobs_idempotency_key ON jobs(idempotency_key)
		WHERE idempotency_key != ''`)
	return err
}

// migrate039JobsClaimLease adds the claim token AcquireNext stamps and the
// lease expiry (Unix milliseconds, NULL = unleased) task workers renew.
// migrate040ProjectionVersion adds objects.projection_version, the
// projection version (indexsig.ProjectionVersion) each row's
// projected_fts_body was derived under, and clears the stored FTS signature
// hash: unstamped rows may hold any earlier projection, so the signature
// no longer vouches for them. Both happen only when the column is added,
// in one transaction.
func migrate040ProjectionVersion(ctx context.Context, d *Driver) error {
	has, err := tableHasColumn(ctx, d, "objects", "projection_version")
	if err != nil || has {
		return err
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`ALTER TABLE objects ADD COLUMN projection_version TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add objects.projection_version: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE index_signatures
		SET signature_hash = '', inputs_summary = ?
		WHERE signature_id = ?`,
		"cleared by migration 040: rows carry no projection_version stamp", indexsig.FTSSignatureID); err != nil {
		return fmt.Errorf("clear fts signature: %w", err)
	}
	return tx.Commit()
}

func migrate039JobsClaimLease(ctx context.Context, d *Driver) error {
	if err := addJobsColumnIfMissing(ctx, d, "claim_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return addJobsColumnIfMissing(ctx, d, "lease_expires_at", "INTEGER")
}

// migrate037PerModelEmbeddings moves embeddings to the per-model index
// schema. Steps 1 and 2 run in one transaction:
//
//  1. Rebuild embeddings with id INTEGER PRIMARY KEY (vec0 rowid linkage;
//     VACUUM may renumber implicit rowids), UNIQUE (object_id, model_id,
//     chunk_idx), and an ON DELETE CASCADE foreign key to objects. Rows of
//     placeholder models and rows whose object is gone are not copied.
//     Skipped when the table already has an id column.
//  2. Delete the legacy-blob placeholder models, their rows and signatures.
//
// Step 3 builds each remaining model's index (ensureEmbeddingIndexes).
func migrate037PerModelEmbeddings(ctx context.Context, d *Driver) error {
	hasID, err := tableHasColumn(ctx, d, "embeddings", "id")
	if err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if !hasID {
		for _, stmt := range []string{
			`CREATE TABLE embeddings_v2 (
			    id          INTEGER PRIMARY KEY,
			    object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			    model_id    TEXT NOT NULL REFERENCES embedding_models(model_id),
			    chunk_idx   INTEGER NOT NULL DEFAULT 0 CHECK (chunk_idx >= 0),
			    vector      BLOB NOT NULL,
			    text        TEXT,
			    created_at  TEXT NOT NULL,
			    UNIQUE (object_id, model_id, chunk_idx)
			)`,
			`INSERT INTO embeddings_v2 (object_id, model_id, chunk_idx, vector, text, created_at)
			     SELECT e.object_id, e.model_id, e.chunk_idx, e.vector, e.text, e.created_at
			       FROM embeddings e
			       JOIN embedding_models m ON m.model_id = e.model_id
			       JOIN objects o          ON o.id = e.object_id
			      WHERE m.provider <> 'legacy-blob'`,
			`DROP TABLE embeddings`,
			`ALTER TABLE embeddings_v2 RENAME TO embeddings`,
			`CREATE INDEX idx_embeddings_model ON embeddings (model_id, object_id)`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("rebuild embeddings: %w", err)
			}
		}
	}

	for _, stmt := range []string{
		`DELETE FROM embeddings
		  WHERE model_id IN (SELECT model_id FROM embedding_models WHERE provider = 'legacy-blob')`,
		`DELETE FROM index_signatures
		  WHERE signature_id IN (SELECT 'embeddings_' || model_id FROM embedding_models WHERE provider = 'legacy-blob')`,
		`DELETE FROM embedding_models WHERE provider = 'legacy-blob'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("remove legacy-blob placeholder: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return ensureEmbeddingIndexes(ctx, d)
}

// migrate038DropSingleVectorPath removes the pre-registry vector storage in
// one transaction: the vec_objects vec0 table (with its shadow tables), the
// object_embeddings table, and the objects.embeddings and
// objects.vector_indexed columns. Per-model embeddings and their indexes are
// untouched. A column still referenced by an index, trigger or view on
// objects fails the migration with that object named, rather than letting
// DROP COLUMN fail on a schema error. Idempotent: every step is guarded.
func migrate038DropSingleVectorPath(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{"vec_objects", "object_embeddings"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	for _, col := range []string{"embeddings", "vector_indexed"} {
		if err := dropObjectsColumn(ctx, tx, col); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// dropObjectsColumn drops objects.col inside tx. A missing column is a
// no-op. A column still referenced by an index, trigger or view on
// objects fails with that object named, rather than letting DROP COLUMN
// fail on a schema error.
func dropObjectsColumn(ctx context.Context, tx *sql.Tx, col string) error {
	has, err := txTableHasColumn(ctx, tx, "objects", col)
	if err != nil {
		return fmt.Errorf("inspect objects.%s: %w", col, err)
	}
	if !has {
		return nil
	}
	var dependent string
	err = tx.QueryRowContext(ctx, `
		SELECT type || ' ' || name FROM sqlite_master
		 WHERE ((type IN ('index', 'trigger') AND tbl_name = 'objects') OR type = 'view')
		   AND sql LIKE '%' || ? || '%'
		 LIMIT 1`, col).Scan(&dependent)
	switch {
	case err == nil:
		return fmt.Errorf("drop objects.%s: still referenced by %s", col, dependent)
	case !errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("inspect dependents of objects.%s: %w", col, err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE objects DROP COLUMN `+col); err != nil {
		return fmt.Errorf("drop objects.%s: %w", col, err)
	}
	return nil
}

// migrate045DropObjectsPlugins drops objects.plugins, a free-form map no
// code reads or writes (ADR-078: no plugin system). One transaction;
// idempotent and guarded like 038.
func migrate045DropObjectsPlugins(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := dropObjectsColumn(ctx, tx, "plugins"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// migrate046ProximityObjectFK rebuilds object_proximity with both
// endpoints referencing objects(id) ON DELETE CASCADE, copying only the
// pairs whose two objects still exist. One transaction; a table that
// already carries the foreign keys is left alone, so a replay is a
// no-op.
func migrate046ProximityObjectFK(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var fks int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_foreign_key_list('object_proximity') WHERE "table" = 'objects'`,
	).Scan(&fks); err != nil {
		return fmt.Errorf("inspect object_proximity foreign keys: %w", err)
	}
	if fks >= 2 {
		return nil
	}
	for _, stmt := range []string{
		`CREATE TABLE object_proximity_fk (
			object_a    TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			object_b    TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			score       REAL NOT NULL,
			semantic    REAL NOT NULL DEFAULT 0,
			temporal    REAL NOT NULL DEFAULT 0,
			entity      REAL NOT NULL DEFAULT 0,
			origin      REAL NOT NULL DEFAULT 0,
			behavioral  REAL NOT NULL DEFAULT 0,
			computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			PRIMARY KEY (object_a, object_b),
			CHECK (object_a < object_b)
		)`,
		`INSERT INTO object_proximity_fk
		 SELECT p.object_a, p.object_b, p.score, p.semantic, p.temporal, p.entity,
		        p.origin, p.behavioral, p.computed_at
		   FROM object_proximity p
		  WHERE EXISTS (SELECT 1 FROM objects o WHERE o.id = p.object_a)
		    AND EXISTS (SELECT 1 FROM objects o WHERE o.id = p.object_b)`,
		`DROP TABLE object_proximity`,
		`ALTER TABLE object_proximity_fk RENAME TO object_proximity`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_a_score ON object_proximity(object_a, score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_b_score ON object_proximity(object_b, score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_score ON object_proximity(score DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_proximity_computed ON object_proximity(computed_at)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild object_proximity: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// perPrincipalTables hold rows that belong to one principal: the instance
// owner's by default, a restricted viewer's once sharing lands. Each has
// an owner_principal column since migration 047.
var perPrincipalTables = []string{"search_history", "saved_searches", "resurfacing_queue"}

// migrate047OwnerPrincipal adds owner_principal TEXT NOT NULL DEFAULT
// 'owner' to every per-principal table that lacks it. Existing rows take
// the default: they are the owner's. Guarded per table, so a replay is a
// no-op.
func migrate047OwnerPrincipal(ctx context.Context, d *Driver) error {
	for _, add := range []struct{ table, ddl string }{
		{"search_history", `ALTER TABLE search_history ADD COLUMN owner_principal TEXT NOT NULL DEFAULT 'owner'`},
		{"saved_searches", `ALTER TABLE saved_searches ADD COLUMN owner_principal TEXT NOT NULL DEFAULT 'owner'`},
		{"resurfacing_queue", `ALTER TABLE resurfacing_queue ADD COLUMN owner_principal TEXT NOT NULL DEFAULT 'owner'`},
	} {
		has, err := tableHasColumn(ctx, d, add.table, "owner_principal")
		if err != nil {
			return err
		}
		if has {
			continue
		}
		if _, err := d.db.ExecContext(ctx, add.ddl); err != nil {
			return fmt.Errorf("add %s.owner_principal: %w", add.table, err)
		}
	}
	return nil
}

// migrate048AuthRegistry creates the principal registry (ADR-079):
// principals, principal_identities, auth_tickets and the kv table that
// holds API key records, and seeds the one owner principal. Times are
// Unix milliseconds (UTC) like ui_sessions. The instance name lives in
// config, not the database, so the owner's display name is "Owner".
// One transaction; IF NOT EXISTS and ON CONFLICT DO NOTHING make a
// replay a no-op.
func migrate048AuthRegistry(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS principals (
			id           TEXT PRIMARY KEY,
			kind         TEXT NOT NULL,
			display_name TEXT NOT NULL,
			roles        TEXT NOT NULL DEFAULT '["reader"]',
			status       TEXT NOT NULL DEFAULT 'active',
			created_at   INTEGER NOT NULL,
			revoked_at   INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS principal_identities (
			id           TEXT PRIMARY KEY,
			principal_id TEXT NOT NULL REFERENCES principals(id),
			method       TEXT NOT NULL,
			issuer       TEXT NOT NULL DEFAULT '',
			subject      TEXT NOT NULL,
			name         TEXT NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL,
			revoked_at   INTEGER,
			last_used_at INTEGER,
			UNIQUE (method, issuer, subject)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_identities_principal ON principal_identities(principal_id)`,
		`CREATE TABLE IF NOT EXISTS auth_tickets (
			hash         TEXT PRIMARY KEY,
			kind         TEXT NOT NULL,
			principal_id TEXT REFERENCES principals(id),
			grants       TEXT NOT NULL DEFAULT '{}',
			created_by   TEXT NOT NULL REFERENCES principals(id),
			created_at   INTEGER NOT NULL,
			expires_at   INTEGER NOT NULL,
			redeemed_at  INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_tickets_expires ON auth_tickets(expires_at)`,
		`CREATE TABLE IF NOT EXISTS kv (
			key        TEXT PRIMARY KEY,
			value      BLOB NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create principal registry: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO principals (id, kind, display_name, roles, status, created_at)
		VALUES (?, ?, 'Owner', '["admin"]', ?, ?) ON CONFLICT(id) DO NOTHING`,
		storage.OwnerPrincipal, storage.PrincipalKindOwner, storage.PrincipalActive, toMillis(time.Now())); err != nil {
		return fmt.Errorf("seed owner principal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// entitlementInboundShapeSQL matches a registry_entitlements row whose
// name is principal-shaped: the owner principal or a minted prn_ ID.
const entitlementInboundShapeSQL = `(registry_name = 'owner' OR substr(registry_name, 1, 4) = 'prn_')`

// migrate051EntitlementKind rebuilds registry_entitlements with a kind
// column and a (kind, registry_name) primary key. Backfill: a
// principal-shaped name (owner, prn_*) becomes one inbound row; any
// other name becomes an outbound row plus an inbound copy. Before this
// migration the inbound gate and outbound sync read the same rows, and
// a static-token principal ("phone") is indistinguishable from a
// registry name, so the copy keeps every principal's gating exactly as
// it was; an operator removes a stray inbound copy with
// 'dpkms entitlement ungate'. A table that already has kind is left
// alone, so a replay is a no-op.
func migrate051EntitlementKind(ctx context.Context, d *Driver) error {
	has, err := tableHasColumn(ctx, d, "registry_entitlements", "kind")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	const cols = `registry_name, plan, namespaces, expires_at, fetched_at`
	for _, stmt := range []string{
		`CREATE TABLE registry_entitlements_kind (
			kind           TEXT NOT NULL CHECK (kind IN ('outbound', 'inbound')),
			registry_name  TEXT NOT NULL,
			plan           TEXT NOT NULL,
			namespaces     TEXT NOT NULL,
			expires_at     TEXT,
			fetched_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			PRIMARY KEY (kind, registry_name)
		)`,
		`INSERT INTO registry_entitlements_kind (kind, ` + cols + `)
		 SELECT 'inbound', ` + cols + ` FROM registry_entitlements WHERE ` + entitlementInboundShapeSQL,
		`INSERT INTO registry_entitlements_kind (kind, ` + cols + `)
		 SELECT 'outbound', ` + cols + ` FROM registry_entitlements WHERE NOT ` + entitlementInboundShapeSQL,
		`INSERT INTO registry_entitlements_kind (kind, ` + cols + `)
		 SELECT 'inbound', ` + cols + ` FROM registry_entitlements WHERE NOT ` + entitlementInboundShapeSQL,
		`DROP TABLE registry_entitlements`,
		`ALTER TABLE registry_entitlements_kind RENAME TO registry_entitlements`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild registry_entitlements: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// migrate050ObjectListIndexes indexes objects.created_at and
// objects.updated_at, the columns List orders by (see the migrations
// entry and objectListConditions).
func migrate050ObjectListIndexes(ctx context.Context, d *Driver) error {
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_objects_created_at ON objects(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_updated_at ON objects(updated_at)`,
	} {
		if _, err := d.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("object list indexes: %w", err)
		}
	}
	return nil
}

// migrate042FederationChangeCursor adds federation_watermarks.last_object_id
// and the objects change-order index. The index expression matches the
// change-order predicate in ObjectStore.List verbatim, so the planner
// uses it.
func migrate042FederationChangeCursor(ctx context.Context, d *Driver) error {
	has, err := tableHasColumn(ctx, d, "federation_watermarks", "last_object_id")
	if err != nil {
		return err
	}
	if !has {
		if _, err := d.db.ExecContext(ctx,
			`ALTER TABLE federation_watermarks ADD COLUMN last_object_id TEXT NOT NULL DEFAULT ''`,
		); err != nil {
			return err
		}
	}
	_, err = d.db.ExecContext(ctx,
		`CREATE INDEX IF NOT EXISTS idx_objects_change_order ON objects(datetime(updated_at), id)`)
	return err
}

// objectChangeSeqTriggers stamp objects.change_seq from
// object_change_counter on every insert and every update. They run in
// the writing statement's transaction, under SQLite's single write lock,
// so change_seq values commit in increasing order: a reader never sees
// a value while a smaller one is still uncommitted. The update trigger's
// WHEN skips the stamping UPDATE itself, which alone changes change_seq.
var objectChangeSeqTriggers = []string{
	`CREATE TRIGGER objects_change_seq_insert AFTER INSERT ON objects
	BEGIN
		UPDATE object_change_counter SET seq = seq + 1 WHERE id = 1;
		UPDATE objects SET change_seq = (SELECT seq FROM object_change_counter WHERE id = 1)
		 WHERE id = NEW.id;
	END`,
	`CREATE TRIGGER objects_change_seq_update AFTER UPDATE ON objects
	WHEN NEW.change_seq = OLD.change_seq
	BEGIN
		UPDATE object_change_counter SET seq = seq + 1 WHERE id = 1;
		UPDATE objects SET change_seq = (SELECT seq FROM object_change_counter WHERE id = 1)
		 WHERE id = NEW.id;
	END`,
}

// migrate043ObjectChangeSeq adds objects.change_seq, backfills it in
// (updated_at, id) order after any value already stamped, seeds the
// counter past the largest, and installs the stamping triggers. The
// watermark table trades its (last_synced_at, last_object_id) position
// for last_change_seq; no position converts, so every target starts
// from 0 and gets every object once more (receivers dedup). Re-runnable.
func migrate043ObjectChangeSeq(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	addColumn := func(table, column, ddl string) error {
		has, err := txTableHasColumn(ctx, tx, table, column)
		if err != nil || has {
			return err
		}
		_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` ADD COLUMN `+column+` `+ddl)
		return err
	}
	dropColumn := func(table, column string) error {
		has, err := txTableHasColumn(ctx, tx, table, column)
		if err != nil || !has {
			return err
		}
		_, err = tx.ExecContext(ctx, `ALTER TABLE `+table+` DROP COLUMN `+column)
		return err
	}

	if err := addColumn("objects", "change_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	stmts := append([]string{
		`DROP TRIGGER IF EXISTS objects_change_seq_insert`,
		`DROP TRIGGER IF EXISTS objects_change_seq_update`,
		`WITH base AS (SELECT COALESCE(MAX(change_seq), 0) AS n FROM objects),
		      r AS (SELECT id, ROW_NUMBER() OVER (ORDER BY datetime(updated_at), id) AS rn
		              FROM objects WHERE change_seq = 0)
		 UPDATE objects SET change_seq = (SELECT n FROM base) + r.rn FROM r WHERE r.id = objects.id`,
		`CREATE TABLE IF NOT EXISTS object_change_counter (
			id  INTEGER PRIMARY KEY CHECK (id = 1),
			seq INTEGER NOT NULL
		)`,
		`INSERT INTO object_change_counter (id, seq)
		 SELECT 1, COALESCE(MAX(change_seq), 0) FROM objects WHERE true
		 ON CONFLICT(id) DO UPDATE SET seq = max(seq, excluded.seq)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_change_seq ON objects(change_seq)`,
		`DROP INDEX IF EXISTS idx_objects_change_order`,
	}, objectChangeSeqTriggers...)
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}

	if err := addColumn("federation_watermarks", "last_change_seq", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for _, c := range []string{"last_object_id", "last_synced_at"} {
		if err := dropColumn("federation_watermarks", c); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// txTableHasColumn is tableHasColumn inside tx.
func txTableHasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	return n > 0, err
}

// tableHasColumn reports whether pragma_table_info(table) lists column.
func tableHasColumn(ctx context.Context, d *Driver, table, column string) (bool, error) {
	var n int
	err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	return n > 0, nil
}

// migrate033EmbeddingsBackfill copies legacy object_embeddings rows into the
// composite-key embeddings table under a synthetic model_id, seeds the
// matching embedding_models row (marked is_default = 1 per ADR-071 §"Backward
// compatibility"), and stamps the embeddings_<model_id> row in
// index_signatures so ADR-070's auto-rebuild semantics apply.
//
// The synthetic model_id rule is "legacy-blob-<dim>@<schema-date>", where:
//   - "legacy-blob" identifies these rows as predating the registry (no
//     provider name was recorded by the legacy single-table schema).
//   - "<dim>" is legacyVectorDimension, the only stable shape input we
//     have for the legacy rows.
//   - "@<schema-date>" follows the ADR-071 model_id convention (provider@date)
//     so downstream tools that parse model_ids don't need a special case.
//
// Idempotent: repeated runs against an already-migrated DB INSERT OR IGNORE
// the composite-key rows and ON CONFLICT-upsert the singleton model row.
// Skipped cleanly when object_embeddings does not exist (fresh installs).
func migrate033EmbeddingsBackfill(ctx context.Context, d *Driver) error {
	dim := legacyVectorDimension

	// Always seed an embedding_models default row, even if there are no
	// legacy object_embeddings rows (fresh installs need a default too so
	// the registry has something to return). The schema-date suffix
	// matches ADR-071's documented convention; we use a fixed wall-clock
	// anchor so the model_id is deterministic across re-runs of the
	// migration on the same DB.
	modelID := legacyEmbeddingModelID(dim)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO embedding_models
			(model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES (?, ?, ?, 1, ?, '{}')
		ON CONFLICT(model_id) DO UPDATE SET
			provider      = excluded.provider,
			dimension     = excluded.dimension,
			is_default    = 1,
			registered_at = embedding_models.registered_at`,
		modelID, "legacy-blob", dim, now,
	); err != nil {
		return fmt.Errorf("seed default embedding_models row: %w", err)
	}

	// Backfill rows from object_embeddings into embeddings, but only when
	// the legacy table actually exists. Fresh installs where migration
	// 004 ran but never produced rows still have the table; only an
	// install whose migration 004 was retroactively dropped would not.
	// We check for existence so the migration is robust either way.
	var n int
	if err := d.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='object_embeddings'`,
	).Scan(&n); err != nil {
		return fmt.Errorf("check legacy object_embeddings table: %w", err)
	}
	if n > 0 {
		if _, err := d.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO embeddings
				(object_id, model_id, chunk_idx, vector, text, created_at)
			SELECT id, ?, 0, embedding, NULL, COALESCE(created_at, ?)
			  FROM object_embeddings`,
			modelID, now,
		); err != nil {
			return fmt.Errorf("backfill embeddings from object_embeddings: %w", err)
		}
	}

	// Stamp the embeddings_<model_id> row in index_signatures (ADR-070
	// integration). Hash inputs follow the ADR-071 §"Data model" callout:
	// "(model_id + dimension + provider)".
	//
	// Defensive CREATE: production DBs that ran migration 029 already
	// have the table, but in-development DBs upgraded across the
	// index-signature / embedding-model boundary may have schema_version recording 029
	// without the table actually present (a known dev-pollution mode).
	// A no-op CREATE TABLE IF NOT EXISTS keeps this migration robust
	// without re-recording the version.
	if _, err := d.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS index_signatures (
		    signature_id   TEXT NOT NULL PRIMARY KEY,
		    signature_hash TEXT NOT NULL,
		    computed_at    TEXT NOT NULL,
		    inputs_summary TEXT NOT NULL DEFAULT ''
		)`); err != nil {
		return fmt.Errorf("ensure index_signatures table exists: %w", err)
	}

	sigID := EmbeddingSignatureID(modelID)
	idx, err := indexsig.SQLiteVectorIndex(ctx, d.db)
	if err != nil {
		return fmt.Errorf("describe vector index for signature: %w", err)
	}
	hash, summary := indexsig.ComputeEmbedding(
		modelID, "legacy-blob", dim, idx.Method, idx.OpsClass, idx.BuildParams)
	if err := UpsertIndexSignature(ctx, d.db, sigID, hash, summary); err != nil {
		return fmt.Errorf("stamp embeddings index_signatures row: %w", err)
	}

	return nil
}

// legacyVectorDimension is the fixed vector dimension of the pre-registry
// single-vector schema (vec_objects, object_embeddings). Only the historic
// migrations 020 and 033-034 read it; 038 drops what they built.
const legacyVectorDimension = 1536

// legacyEmbeddingModelID returns the synthetic model_id
// migrate033EmbeddingsBackfill gave rows migrated from the pre-registry
// single-table schema. 037 deletes that placeholder model again.
func legacyEmbeddingModelID(dim int) string {
	// Fixed wall-clock anchor: ADR-071 was authored on 2026-05-07.
	// Using a fixed date (rather than time.Now()) keeps the synthetic
	// model_id deterministic — re-running the migration on the same DB
	// must compute the same key.
	return fmt.Sprintf("legacy-blob-%d@2026-05-07", dim)
}

// EmbeddingSignatureID returns the index_signatures row key for an embedding
// model. Mirrors FTSSignatureID for the FTS path.
func EmbeddingSignatureID(modelID string) string {
	return indexsig.EmbeddingSignatureID(modelID)
}

// migrate049SessionsBoundToIdentities rebuilds ui_sessions and
// ui_login_codes with identity_id REFERENCES principal_identities(id)
// in place of token_hash. Every existing row was minted by a static
// token, which no longer authenticates, so none is carried over: the
// browsers sign in again with a key. Idempotent: a table that already
// has identity_id is left alone.
func migrate049SessionsBoundToIdentities(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, t := range []struct{ name, create, index string }{
		{"ui_sessions", `CREATE TABLE ui_sessions (
			id              TEXT PRIMARY KEY,
			secret_hash     TEXT NOT NULL UNIQUE,
			principal_id    TEXT NOT NULL,
			identity_id     TEXT NOT NULL REFERENCES principal_identities(id),
			scope           TEXT NOT NULL,
			user_agent      TEXT NOT NULL DEFAULT '',
			remote_addr     TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL,
			last_seen_at    INTEGER NOT NULL,
			idle_expires_at INTEGER NOT NULL,
			expires_at      INTEGER NOT NULL,
			revoked_at      INTEGER,
			revoke_reason   TEXT NOT NULL DEFAULT ''
		)`, `CREATE INDEX IF NOT EXISTS idx_ui_sessions_identity ON ui_sessions(identity_id);
		CREATE INDEX IF NOT EXISTS idx_ui_sessions_principal ON ui_sessions(principal_id)`},
		{"ui_login_codes", `CREATE TABLE ui_login_codes (
			code_hash    TEXT PRIMARY KEY,
			principal_id TEXT NOT NULL,
			identity_id  TEXT NOT NULL REFERENCES principal_identities(id),
			scope        TEXT NOT NULL,
			created_at   INTEGER NOT NULL,
			expires_at   INTEGER NOT NULL
		)`, `CREATE INDEX IF NOT EXISTS idx_ui_login_codes_identity ON ui_login_codes(identity_id)`},
	} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'identity_id'`, t.name).Scan(&n); err != nil {
			return fmt.Errorf("inspect %s: %w", t.name, err)
		}
		if n > 0 {
			continue
		}
		for _, stmt := range []string{`DROP TABLE IF EXISTS ` + t.name, t.create, t.index} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("rebuild %s: %w", t.name, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
