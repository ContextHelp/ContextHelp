package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	sqldriver "database/sql/driver"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
)

// pgvectorUnavailable wraps a CREATE EXTENSION vector failure in a single
// actionable error. On managed Postgres (RDS, Cloud SQL, Azure, …) the
// extension often must be enabled via the provider console before the
// connecting role may CREATE EXTENSION — this is the first error every
// self-hosting operator would otherwise hit as a raw migration failure.
func pgvectorUnavailable(err error) error {
	return fmt.Errorf("pgvector extension unavailable: enable it on your Postgres instance (run CREATE EXTENSION vector as a privileged role, or toggle the extension in your provider's console) — %w", err)
}

// pgMigration is one versioned entry in the Postgres schema ledger,
// mirroring the SQLite schema_version pattern: ordered, append-only,
// recorded per-version so schema work lands as numbered steps instead of
// ad-hoc idempotent helpers (the failure mode that produced write-path
// columns no migration created).
//
// Every entry must stay idempotent: databases initialized before the
// ledger existed have the full schema but no schema_version table, and
// adopting them replays the whole chain once.
type pgMigration struct {
	Version int
	Name    string
	// Statements are executed in order when fn is nil.
	Statements []string
	// fn covers migrations that need Go-level logic (info-schema checks,
	// seeded rows, dynamic DDL).
	fn func(ctx context.Context, d *Driver) error
}

// pgMigrations is the versioned chain. Versions 1-6 fold the historical
// unversioned bootstrap (identical statements, identical order) so
// already-initialized databases replay them as no-ops; versions 7+ are the
// fixes the unversioned era lost.
var pgMigrations = []pgMigration{
	{Version: 1, Name: "baseline schema", Statements: baselineSchema},
	{Version: 2, Name: "graph canonical", fn: func(ctx context.Context, d *Driver) error {
		return migrateGraphCanonical(ctx, d.db)
	}},
	{Version: 3, Name: "jobs.user_mentions", fn: func(ctx context.Context, d *Driver) error {
		return migrateJobsUserMentions(ctx, d.db)
	}},
	{Version: 4, Name: "jobs.user_hints", fn: func(ctx context.Context, d *Driver) error {
		return migrateJobsUserHints(ctx, d.db)
	}},
	{Version: 5, Name: "jobs.user_profile+user_note", fn: func(ctx context.Context, d *Driver) error {
		return migrateJobsUserProfileNote(ctx, d.db)
	}},
	{Version: 6, Name: "embedding models default seed", fn: migrateEmbeddingsDefaultSeed},
	// External dedup key (Slack ts, tweet ID, email message-id, …). The
	// column was referenced by every object query but never created by the
	// unversioned bootstrap — the canonical fresh-database break.
	{Version: 7, Name: "objects.source_key", Statements: []string{
		`ALTER TABLE objects ADD COLUMN IF NOT EXISTS source_key TEXT DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_objects_source_key ON objects(source_key) WHERE source_key <> ''`,
	}},
	// Profile-scoped objects; profile_id = '' means the global scope.
	// Mirrors the SQLite column so profile semantics stop silently
	// flattening into the global namespace on this backend.
	{Version: 8, Name: "objects.profile_id", Statements: []string{
		`ALTER TABLE objects ADD COLUMN IF NOT EXISTS profile_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_objects_profile_id ON objects (profile_id)`,
	}},
	// FTS half of the search schema: the projected body column (single
	// source of indexed text, mirroring SQLite migration 023) plus a STORED
	// generated tsvector over it with a GIN index. Generated-column
	// maintenance replaces SQLite's manual delete+reinsert into the FTS
	// virtual table and cannot drift from the row.
	{Version: 9, Name: "objects.projected_fts_body + generated tsvector + GIN", Statements: []string{
		`ALTER TABLE objects ADD COLUMN IF NOT EXISTS projected_fts_body TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE objects ADD COLUMN IF NOT EXISTS fts tsvector
			GENERATED ALWAYS AS (to_tsvector('` + ftsRegconfig + `', projected_fts_body)) STORED`,
		`CREATE INDEX IF NOT EXISTS idx_objects_fts ON objects USING GIN (fts)`,
	}},
	// Honest vector schema: re-type the dead embeddings.vector BYTEA (a
	// copy-paste of the SQLite BLOB column that no Postgres query path can
	// read) to a real pgvector column, and delete the phantom index
	// signature stamped for the index that never existed.
	{Version: 10, Name: "embeddings.vector BYTEA -> pgvector", fn: migrateEmbeddingsHonestVector},
	// Vector dimension + ANN index: apply legacyVectorDimension to
	// objects.embedding (dynamic DDL, the analog of SQLite's
	// {DIMENSION}-templated vec0 migration) and replace the legacy ivfflat
	// index with HNSW cosine ops. Dropped by 15.
	{Version: 11, Name: "objects.embedding dimension + HNSW cosine index", fn: migrateObjectsEmbeddingDimension},
	// ADR-070 provenance for the vector index: stamp the
	// embeddings_<default-model> signature from the live index description
	// so the index_signatures table describes a real index on this backend
	// (the phantom BYTEA stamp was deleted by migration 10).
	{Version: 12, Name: "stamp vector index signature for default model", fn: migrateStampVectorSignature},
	// Client-replay dedup: idempotency_key column + partial unique index
	// so a re-submitted enqueue (response lost in transit) resolves to the
	// existing job instead of minting a duplicate.
	{Version: 13, Name: "jobs.idempotency_key", fn: func(ctx context.Context, d *Driver) error {
		return migrateJobsIdempotencyKey(ctx, d.db)
	}},
	// Per-model embeddings (ADR-071 amendment 2026-09-26): object FK with
	// cascade, non-negative chunk CHECK, placeholder removal, and one
	// partial HNSW index per registered model. Migrate re-runs the
	// per-model pass on every open.
	{Version: 14, Name: "per-model embeddings", fn: migratePerModelEmbeddings},
	// Drop the single-vector path (ADR-071 amendment 2026-09-26): vectors
	// live only in embeddings. Dropping objects.embedding also drops its
	// HNSW index. objects.vector_indexed goes with it: it went stale on
	// every default flip, and coverage comes from embeddings.
	{Version: 15, Name: "drop single-vector path", Statements: []string{
		`ALTER TABLE objects DROP COLUMN IF EXISTS embedding`,
		`ALTER TABLE objects DROP COLUMN IF EXISTS vector_indexed`,
	}},
	// Extracted text, mirroring the SQLite column. Rows written before this
	// entry never stored TextContent, so the backfill applies the
	// projection.BodyText rule to what was stored: text_content, else
	// raw_content.
	{Version: 16, Name: "objects.text_content", Statements: []string{
		`ALTER TABLE objects ADD COLUMN IF NOT EXISTS text_content TEXT NOT NULL DEFAULT ''`,
		`UPDATE objects SET text_content = raw_content WHERE text_content = '' AND raw_content <> ''`,
	}},
	// Task job claim + lease: AcquireNext stamps claim_token; a worker
	// running a task job renews lease_expires_at (database clock), and
	// stale recovery leaves a leased job alone until the lease runs out,
	// so a second dpkms on the same database cannot requeue and re-run
	// a job another dpkms is running.
	{Version: 17, Name: "jobs.claim_token + lease_expires_at", Statements: []string{
		`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS claim_token TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ`,
	}},
}

// migratePerModelEmbeddings moves embeddings to the per-model index schema.
// Steps 1-3 run in one transaction:
//
//  1. Delete rows whose object is gone and rows of legacy-blob models.
//  2. Add embeddings_object_fk (ON DELETE CASCADE) and
//     embeddings_chunk_idx_nonneg, each guarded by a pg_constraint lookup.
//  3. Delete the legacy-blob placeholder models and their signatures.
//
// Step 4 builds each remaining model's index (ensureEmbeddingIndexes).
func migratePerModelEmbeddings(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM embeddings e
		 WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.id = e.object_id)
		    OR e.model_id IN (SELECT model_id FROM embedding_models WHERE provider = 'legacy-blob')`); err != nil {
		return fmt.Errorf("delete orphaned and placeholder embeddings: %w", err)
	}
	for _, c := range []struct{ name, ddl string }{
		{"embeddings_object_fk", `ALTER TABLE embeddings ADD CONSTRAINT embeddings_object_fk
			FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE`},
		{"embeddings_chunk_idx_nonneg", `ALTER TABLE embeddings ADD CONSTRAINT embeddings_chunk_idx_nonneg
			CHECK (chunk_idx >= 0)`},
	} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_constraint
			                WHERE conname = $1 AND conrelid = 'embeddings'::regclass)`,
			c.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect %s: %w", c.name, err)
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, c.ddl); err != nil {
			return fmt.Errorf("add %s: %w", c.name, err)
		}
	}
	for _, stmt := range []string{
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

// migrateStampVectorSignature computes the embedding signature for the
// default registered model from the live ANN index description (method, ops
// class, build params via pg_get_indexdef) and upserts the ADR-070 stamp.
// No default model means nothing to stamp — not an error.
func migrateStampVectorSignature(ctx context.Context, d *Driver) error {
	var (
		modelID, provider string
		dim               int
	)
	err := d.db.QueryRowContext(ctx, `
		SELECT model_id, provider, dimension
		  FROM embedding_models WHERE is_default = 1`).Scan(&modelID, &provider, &dim)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("read default embedding model: %w", err)
	}

	idx, err := indexsig.PostgresVectorIndex(ctx, d.db)
	if err != nil {
		return fmt.Errorf("describe vector index for signature: %w", err)
	}
	hash, summary := indexsig.ComputeEmbedding(
		modelID, provider, dim, idx.Method, idx.OpsClass, idx.BuildParams)
	if err := indexsig.Upsert(ctx, d.db, indexsig.DialectPostgres,
		indexsig.EmbeddingSignatureID(modelID), hash, summary); err != nil {
		return fmt.Errorf("stamp vector index signature: %w", err)
	}
	return nil
}

// ftsRegconfig is the text-search configuration for the generated tsvector
// column and every tsquery built against it. 'simple' is deliberate: it does
// no stemming and no stop-word removal, matching SQLite FTS5's default
// unicode61 tokenizer semantics so both drivers agree on what matches. This
// is the tokenizer-analog decision and feeds the FTS index signature.
// Aliased from the shared ftsq constant (which the compiler's similar==
// tsqueries also bind) so the generated column and every query against it
// can never disagree.
const ftsRegconfig = ftsq.PostgresRegconfig

// hnswMaxDimension is pgvector's HNSW index ceiling. Columns above it (up
// to 4000 with halfvec, which this driver does not use yet) fall back to
// sequential scans — mirroring SQLite's brute-force path for non-standard
// dimensions.
const hnswMaxDimension = 2000

// migrateEmbeddingsHonestVector re-types embeddings.vector from BYTEA to
// pgvector `vector` (typmod-less: per-model dimensions are a first-class
// expectation of the registry). The BYTEA column was write-only dead weight
// — nothing ever read or wrote it — but the migration still refuses to drop
// a column that somehow holds data. Also deletes the phantom
// embeddings_<model_id> index signature stamped for the index that never
// existed: provenance rows must describe real indexes only.
func migrateEmbeddingsHonestVector(ctx context.Context, d *Driver) error {
	var udt string
	err := d.db.QueryRowContext(ctx, `
		SELECT udt_name FROM information_schema.columns
		 WHERE table_name = 'embeddings' AND column_name = 'vector'`).Scan(&udt)
	if err != nil {
		return fmt.Errorf("inspect embeddings.vector: %w", err)
	}
	if udt != "vector" {
		var n int
		if err := d.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM embeddings`).Scan(&n); err != nil {
			return fmt.Errorf("count embeddings rows: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("embeddings.vector is %s and the table holds %d rows; refusing to drop data — export and clear the table, then re-run migration", udt, n)
		}
		if _, err := d.db.ExecContext(ctx,
			`ALTER TABLE embeddings DROP COLUMN vector`); err != nil {
			return fmt.Errorf("drop BYTEA vector column: %w", err)
		}
		if _, err := d.db.ExecContext(ctx,
			`ALTER TABLE embeddings ADD COLUMN vector vector NOT NULL`); err != nil {
			return fmt.Errorf("add pgvector vector column: %w", err)
		}
	}
	if _, err := d.db.ExecContext(ctx,
		`DELETE FROM index_signatures WHERE signature_id LIKE 'embeddings\_%'`); err != nil {
		return fmt.Errorf("delete phantom embeddings signatures: %w", err)
	}
	return nil
}

// legacyVectorDimension is the fixed vector dimension of the pre-registry
// objects.embedding column. Only the historic migrations 6 and 11 read it;
// 15 drops the column.
const legacyVectorDimension = 1536

// migrateObjectsEmbeddingDimension applies legacyVectorDimension to
// objects.embedding and builds the ANN index.
//
// Dimension: the baseline schema hardcoded vector(1536); this entry re-types
// the column to legacyVectorDimension when they differ, refusing when
// stored embeddings exist (they cannot be cast across dimensions — rebuild
// embeddings first, exactly the flow ADR-070 signatures drive).
//
// Index: HNSW with cosine ops — cosine is the pinned cross-driver distance
// contract. HNSW indexes cap at 2000 dimensions (halfvec extends to 4000);
// larger dimensions skip the index and search via sequential scan.
func migrateObjectsEmbeddingDimension(ctx context.Context, d *Driver) error {
	dim := legacyVectorDimension

	var colType string
	err := d.db.QueryRowContext(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod)
		  FROM pg_attribute a
		 WHERE a.attrelid = 'objects'::regclass
		   AND a.attname = 'embedding' AND NOT a.attisdropped`).Scan(&colType)
	if err != nil {
		return fmt.Errorf("inspect objects.embedding: %w", err)
	}

	// The legacy ivfflat index predates the pinned metric contract. Drop it
	// BEFORE any re-type: ALTER COLUMN TYPE rebuilds dependent indexes, and
	// ivfflat refuses columns above 2000 dimensions — the re-type to a
	// larger dimension would fail on the index it is about to obsolete.
	if _, err := d.db.ExecContext(ctx,
		`DROP INDEX IF EXISTS idx_objects_embedding`); err != nil {
		return fmt.Errorf("drop legacy ivfflat index: %w", err)
	}

	want := fmt.Sprintf("vector(%d)", dim)
	if colType != want {
		var n int
		if err := d.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM objects WHERE embedding IS NOT NULL`).Scan(&n); err != nil {
			return fmt.Errorf("count stored embeddings: %w", err)
		}
		if n > 0 {
			return fmt.Errorf("objects.embedding is %s with %d stored vectors; cannot re-type to %s — clear or rebuild embeddings first", colType, n, want)
		}
		ddl := strings.ReplaceAll(
			`ALTER TABLE objects ALTER COLUMN embedding TYPE vector({DIMENSION}) USING embedding::vector({DIMENSION})`,
			"{DIMENSION}", strconv.Itoa(dim))
		if _, err := d.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("re-type objects.embedding to %s: %w", want, err)
		}
	}
	if dim <= hnswMaxDimension {
		if _, err := d.db.ExecContext(ctx,
			`CREATE INDEX IF NOT EXISTS idx_objects_embedding_hnsw
			   ON objects USING hnsw (embedding vector_cosine_ops)`); err != nil {
			return fmt.Errorf("create hnsw cosine index: %w", err)
		}
	}
	return nil
}

// migrateLockKey is the pg_advisory_lock key serializing Migrate across
// concurrent initializers (multiple daemons, CLI + daemon, test fan-out).
// Arbitrary but stable — "ctxt" in ASCII shifted into the upper half of the
// int64 space to avoid colliding with small hand-picked keys other tooling
// tends to use. Advisory locks are database-scoped, so distinct databases
// on one server migrate independently.
const migrateLockKey int64 = 0x63747874 << 20 // "ctxt"

func (d *Driver) Migrate(ctx context.Context) error {
	return d.migrateThrough(ctx, pgMigrations[len(pgMigrations)-1].Version)
}

// migrateThrough is Migrate stopping the ledger at target. Tests pass an
// older version to build a database at a historic schema.
func (d *Driver) migrateThrough(ctx context.Context, target int) error {
	// Serialize concurrent initializers. Without the lock two Migrate calls
	// race everything downstream: CREATE EXTENSION / CREATE TABLE IF NOT
	// EXISTS collide on catalog unique indexes (23505), and both read the
	// same MAX(version) then fight over the schema_version PK. Advisory
	// locks are session-scoped, so the lock lives on a dedicated connection
	// held for the whole migration pass; the loop itself keeps using the
	// pool — mutual exclusion is what matters, not which session runs DDL.
	conn, err := d.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		// Release even when ctx is already cancelled — conn.Close only
		// returns the session to the pool, and a pooled session that still
		// holds the lock would deadlock every later initializer.
		if _, uerr := conn.ExecContext(context.WithoutCancel(ctx),
			`SELECT pg_advisory_unlock($1)`, migrateLockKey); uerr != nil {
			// Poison the session so the pool discards it; the server then
			// drops the lock with the backend.
			_ = conn.Raw(func(any) error { return sqldriver.ErrBadConn })
		}
	}()

	// The vector extension is a hard requirement (embeddings.vector is a
	// pgvector column). Degrade its failure to one clear error instead of a
	// numbered migration failure. Runs outside the ledger: it is
	// server-level state, not schema history.
	if _, err := d.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return pgvectorUnavailable(err)
	}

	if _, err := d.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}

	var current int
	if err := d.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for _, m := range pgMigrations {
		if m.Version <= current || m.Version > target {
			continue
		}
		if m.fn != nil {
			if err := m.fn(ctx, d); err != nil {
				return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
			}
		} else {
			for _, stmt := range m.Statements {
				if _, err := d.db.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("apply migration %d (%s): %w", m.Version, m.Name, err)
				}
			}
		}
		if _, err := d.db.ExecContext(ctx,
			`INSERT INTO schema_version (version) VALUES ($1)`, m.Version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
	}

	// Capability detection, not schema: pgvector >= 0.8.0 ships
	// hnsw.iterative_scan, which the search paths enable per query so a
	// filtered KNN cannot under-return below LIMIT (pgvector applies WHERE
	// after index traversal). Re-detected on every Migrate so an extension
	// upgrade is picked up.
	var extVersion string
	if err := d.db.QueryRowContext(ctx,
		`SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&extVersion); err != nil {
		return fmt.Errorf("detect pgvector version: %w", err)
	}
	d.caps.iterativeScan = pgvectorSupportsIterativeScan(extVersion)

	// ADR-070 verify-and-rebuild for every registered model's vector index,
	// on every open, still under the migration lock.
	if err := ensureEmbeddingIndexes(ctx, d); err != nil {
		return fmt.Errorf("ensure embedding indexes: %w", err)
	}
	return nil
}

// baselineSchema is the pre-ledger bootstrap, frozen as versioned entry 1.
// Do not extend it — append new pgMigrations entries instead.
var baselineSchema = []string{
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
	// ADR-071 Phase 1 (T-0582): embedding_models registry +
	// composite-key embeddings table. Mirrors the sqlite migration
	// 032/033 surface; the partial unique index uses postgres'
	// "WHERE" partial-index syntax and the ON CONFLICT path on
	// the seeded default row matches the sqlite semantics.
	`CREATE TABLE IF NOT EXISTS embedding_models (
			model_id      TEXT PRIMARY KEY,
			provider      TEXT NOT NULL DEFAULT '',
			dimension     INTEGER NOT NULL DEFAULT 0,
			is_default    INTEGER NOT NULL DEFAULT 0,
			registered_at TIMESTAMPTZ NOT NULL,
			deprecated_at TIMESTAMPTZ,
			config_json   JSONB NOT NULL DEFAULT '{}'
		)`,
	`CREATE TABLE IF NOT EXISTS embeddings (
			object_id  TEXT NOT NULL,
			model_id   TEXT NOT NULL REFERENCES embedding_models(model_id),
			chunk_idx  INTEGER NOT NULL DEFAULT 0,
			vector     BYTEA NOT NULL,
			text       TEXT,
			created_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (object_id, model_id, chunk_idx)
		)`,
	`CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings (model_id, object_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_embedding_default
			ON embedding_models(is_default) WHERE is_default = 1`,
	// ADR-070 §3 + ADR-071 §"Data model" callout: per-table
	// signature stamps. Matches the sqlite migration 029 schema.
	`CREATE TABLE IF NOT EXISTS index_signatures (
			signature_id   TEXT PRIMARY KEY,
			signature_hash TEXT NOT NULL,
			computed_at    TIMESTAMPTZ NOT NULL,
			inputs_summary TEXT NOT NULL DEFAULT ''
		)`,
}

// migrateEmbeddingsDefaultSeed inserts the legacy default embedding model row
// (matching the sqlite synthetic-model-id rule). Idempotent — ON CONFLICT DO
// NOTHING so re-running on an upgraded DB is a no-op.
//
// The synthetic model_id matches the sqlite-side rule from
// migrate033EmbeddingsBackfill so a postgres-to-sqlite or sqlite-to-postgres
// snapshot keeps the same model_id surface. No index signature is stamped
// here: signatures describe real indexes, and the composite embeddings table
// has none yet on this backend.
func migrateEmbeddingsDefaultSeed(ctx context.Context, d *Driver) error {
	dim := legacyVectorDimension
	modelID := fmt.Sprintf("legacy-blob-%d@2026-05-07", dim)
	if _, err := d.db.ExecContext(ctx, `
		INSERT INTO embedding_models
			(model_id, provider, dimension, is_default, registered_at, config_json)
		VALUES ($1, $2, $3, 1, NOW(), '{}'::jsonb)
		ON CONFLICT (model_id) DO NOTHING`,
		modelID, "legacy-blob", dim,
	); err != nil {
		return fmt.Errorf("seed default embedding_models row: %w", err)
	}
	return nil
}

// migrateJobsUserMentions adds user_mentions TEXT column to jobs. Idempotent
// via information_schema check so re-running on upgraded DBs is a no-op.
func migrateJobsUserMentions(ctx context.Context, db *sql.DB) error {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'jobs' AND column_name = 'user_mentions'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check user_mentions column: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE jobs ADD COLUMN user_mentions TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add user_mentions column: %w", err)
	}
	return nil
}

// migrateJobsUserHints adds user_hints TEXT column to jobs. Idempotent via
// information_schema check so re-running on upgraded DBs is a no-op.
// Mirrors migrateJobsUserMentions exactly (T-0573).
func migrateJobsUserHints(ctx context.Context, db *sql.DB) error {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'jobs' AND column_name = 'user_hints'
		)
	`).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check user_hints column: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx,
		`ALTER TABLE jobs ADD COLUMN user_hints TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add user_hints column: %w", err)
	}
	return nil
}

// migrateJobsUserProfileNote adds user_profile + user_note TEXT columns to
// jobs. Idempotent per-column via information_schema checks; either
// column can be missing independently after a partial upgrade. Mirrors
// migrateJobsUserMentions / migrateJobsUserHints (T-0588).
//
// Unrolled to two constant ALTER statements (rather than looping with
// string concatenation) so gosec's G201 SQL-injection lint stays
// satisfied without a #nosec annotation. Postgres doesn't accept
// parameterized DDL — column names can't be $1 — so the literal-SQL
// shape is unavoidable.
func migrateJobsUserProfileNote(ctx context.Context, db *sql.DB) error {
	if err := addJobsColumnIfMissing(ctx, db, "user_profile",
		`ALTER TABLE jobs ADD COLUMN user_profile TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if err := addJobsColumnIfMissing(ctx, db, "user_note",
		`ALTER TABLE jobs ADD COLUMN user_note TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	return nil
}

// migrateJobsIdempotencyKey adds the idempotency_key column plus a partial
// unique index over non-empty keys — the race-window guard behind the
// lookup-then-insert dedupe at the enqueue surface. Empty keys (legacy rows,
// keyless enqueues) are exempt.
func migrateJobsIdempotencyKey(ctx context.Context, db *sql.DB) error {
	if err := addJobsColumnIfMissing(ctx, db, "idempotency_key",
		`ALTER TABLE jobs ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS
		idx_jobs_idempotency_key ON jobs(idempotency_key)
		WHERE idempotency_key != ''`)
	if err != nil {
		return fmt.Errorf("create idempotency key index: %w", err)
	}
	return nil
}

// addJobsColumnIfMissing runs alterSQL only when the named column is
// missing from the jobs table. The column-existence check uses a
// parameterized query against information_schema (safe); the alterSQL
// is a literal string supplied by the caller (no operator input).
func addJobsColumnIfMissing(ctx context.Context, db *sql.DB, col, alterSQL string) error {
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'jobs' AND column_name = $1
		)
	`, col).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check %s column: %w", col, err)
	}
	if exists {
		return nil
	}
	if _, err := db.ExecContext(ctx, alterSQL); err != nil {
		return fmt.Errorf("add %s column: %w", col, err)
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
type ObjectStore struct {
	db   *sql.DB
	caps *pgCaps
}
type (
	EntityStore   struct{ db *sql.DB }
	EdgeStore     struct{ db *sql.DB }
	JobStore      struct{ db *sql.DB }
	PipelineStore struct{ db *sql.DB }
	StepStore     struct{ db *sql.DB }
	RegistryStore struct{ db *sql.DB }
	ReminderStore struct{ db *sql.DB }
	FeedStore     struct{ db *sql.DB }
	FeedItemStore struct{ db *sql.DB }
	BatchStore    struct{ db *sql.DB }
)
