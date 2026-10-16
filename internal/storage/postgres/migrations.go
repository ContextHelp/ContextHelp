package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	sqldriver "database/sql/driver"

	"github.com/ideacrafterslabs/ctxt/internal/search/ftsq"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
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
	// Per-row projection_version stamp. Rows written before it carry no
	// stamp, so the stored FTS signature is cleared with it: the next start
	// reports a mismatch and schedules the re-projection job, which stamps
	// rows and signature again.
	{Version: 18, Name: "objects.projection_version", fn: migrateProjectionVersion},
	// Web UI browser sessions and their one-time login codes, mirroring
	// SQLite migration 041. Only SHA-256 hashes of the cookie secret, the
	// code and the minting static token are stored. The token_hash index
	// is guarded on the column: migration 29 drops token_hash, so a replay
	// over the swapped tables skips it instead of failing; a database that
	// has not reached 29 gets the index exactly as before.
	{Version: 19, Name: "ui_sessions + ui_login_codes", Statements: []string{
		`CREATE TABLE IF NOT EXISTS ui_sessions (
			id              TEXT PRIMARY KEY,
			secret_hash     TEXT NOT NULL UNIQUE,
			principal_id    TEXT NOT NULL,
			token_hash      TEXT NOT NULL,
			scope           TEXT NOT NULL,
			user_agent      TEXT NOT NULL DEFAULT '',
			remote_addr     TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL,
			last_seen_at    TIMESTAMPTZ NOT NULL,
			idle_expires_at TIMESTAMPTZ NOT NULL,
			expires_at      TIMESTAMPTZ NOT NULL,
			revoked_at      TIMESTAMPTZ,
			revoke_reason   TEXT NOT NULL DEFAULT ''
		)`,
		`DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = 'ui_sessions' AND column_name = 'token_hash') THEN
				CREATE INDEX IF NOT EXISTS idx_ui_sessions_token_hash ON ui_sessions (token_hash);
			END IF;
		END $$`,
		`CREATE INDEX IF NOT EXISTS idx_ui_sessions_principal ON ui_sessions (principal_id)`,
		`CREATE TABLE IF NOT EXISTS ui_login_codes (
			code_hash    TEXT PRIMARY KEY,
			principal_id TEXT NOT NULL,
			token_hash   TEXT NOT NULL,
			scope        TEXT NOT NULL,
			created_at   TIMESTAMPTZ NOT NULL,
			expires_at   TIMESTAMPTZ NOT NULL
		)`,
	}},
	// Thin entities (UpsertThin: registry thin sync, mentions) were stored
	// with metadata '[]' instead of '{}', so every read of one failed to
	// decode into the metadata map. Only the empty array is rewritten: no
	// writer ever stored a non-empty array, and one would still fail loudly.
	{Version: 20, Name: "entities.metadata empty array -> object", Statements: []string{
		`UPDATE entities SET metadata = '{}'::jsonb WHERE metadata = '[]'::jsonb`,
	}},
	// Federation watermarks become change positions, mirroring SQLite
	// migration 042: last_synced_at holds the updated_at and
	// last_object_id the id of the last object a target acknowledged;
	// objects get an index in that change order.
	{Version: 21, Name: "federation change cursor", Statements: []string{
		`ALTER TABLE federation_watermarks ADD COLUMN IF NOT EXISTS last_object_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_objects_change_order ON objects (updated_at, (id COLLATE "C"))`,
	}},
	// objects.change_seq, mirroring SQLite migration 043: a change
	// position stamped from a counter on every insert and update,
	// backfilled in (updated_at, id) order. Watermarks become the last
	// acknowledged change_seq; no position converts, so targets start
	// from 0 (receivers dedup the re-sent objects).
	{Version: 22, Name: "objects.change_seq", Statements: objectChangeSeqStatements},
	// aliases.object_id -> objects(id) ON DELETE CASCADE, mirroring SQLite
	// migration 010: without it an alias outlived its object and kept
	// resolving to a deleted id. Orphans are deleted before the constraint.
	{Version: 23, Name: "aliases object FK with cascade", fn: migrateAliasesObjectFK},
	// Approval queue for entities untrusted registries serve, mirroring
	// SQLite migration 044.
	{Version: 24, Name: "registry_pending_entities", Statements: []string{
		`CREATE TABLE IF NOT EXISTS registry_pending_entities (
			id               TEXT PRIMARY KEY,
			registry_name    TEXT NOT NULL,
			registry_url     TEXT NOT NULL,
			slug             TEXT NOT NULL,
			namespace        TEXT NOT NULL DEFAULT '',
			title            TEXT NOT NULL DEFAULT '',
			version          TEXT NOT NULL,
			payload          JSONB NOT NULL,
			signature_status TEXT NOT NULL,
			first_seen_at    TIMESTAMPTZ NOT NULL,
			last_seen_at     TIMESTAMPTZ NOT NULL,
			UNIQUE (registry_name, slug, version)
		)`,
	}},
	// Drop objects.plugins, mirroring SQLite migration 045: there is no
	// plugin system (ADR-078) and no code reads or writes the column.
	// Not part of the tsvector expression, so no re-projection follows.
	{Version: 25, Name: "drop objects.plugins", Statements: []string{
		`ALTER TABLE objects DROP COLUMN IF EXISTS plugins`,
	}},
	// object_proximity endpoints reference objects with ON DELETE
	// CASCADE, mirroring SQLite migration 046: deleting an object drops
	// its proximity pairs. Pairs whose object is already gone go first.
	{Version: 26, Name: "object_proximity object FKs with cascade", fn: migrateProximityObjectFK},
	// owner_principal on the per-principal tables, mirroring SQLite
	// migration 047. Postgres had no search_history, saved_searches or
	// resurfacing_queue (their stores are placeholders), so the tables
	// are created here, after SQLite's 017 / 021 DDL, with the column in
	// place; the ALTERs cover a table that already exists without it.
	{Version: 27, Name: "owner_principal on per-principal tables", Statements: []string{
		`CREATE TABLE IF NOT EXISTS saved_searches (
			id              TEXT PRIMARY KEY,
			name            TEXT NOT NULL UNIQUE,
			query           TEXT NOT NULL,
			profile_id      TEXT NOT NULL DEFAULT '',
			alert_on        TEXT NOT NULL DEFAULT '',
			notify          TEXT NOT NULL DEFAULT '',
			owner_principal TEXT NOT NULL DEFAULT 'owner',
			created_at      TIMESTAMPTZ NOT NULL,
			updated_at      TIMESTAMPTZ NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS search_history (
			id              TEXT PRIMARY KEY,
			query           TEXT NOT NULL,
			profile_id      TEXT NOT NULL DEFAULT '',
			strategies_used TEXT NOT NULL DEFAULT '',
			result_count    INTEGER NOT NULL DEFAULT 0,
			owner_principal TEXT NOT NULL DEFAULT 'owner',
			searched_at     TIMESTAMPTZ NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_search_history_profile_id ON search_history (profile_id)`,
		`CREATE INDEX IF NOT EXISTS idx_search_history_searched_at ON search_history (searched_at DESC)`,
		`CREATE TABLE IF NOT EXISTS resurfacing_queue (
			id              TEXT PRIMARY KEY,
			object_id       TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
			profile_id      TEXT NOT NULL,
			score           DOUBLE PRECISION NOT NULL,
			reason          TEXT NOT NULL,
			owner_principal TEXT NOT NULL DEFAULT 'owner',
			surfaced_at     TIMESTAMPTZ,
			dismissed_at    TIMESTAMPTZ,
			created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_profile_id ON resurfacing_queue (profile_id)`,
		`CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_object_id ON resurfacing_queue (object_id)`,
		`CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_unseen ON resurfacing_queue (profile_id, score)
			WHERE surfaced_at IS NULL AND dismissed_at IS NULL`,
		`ALTER TABLE saved_searches ADD COLUMN IF NOT EXISTS owner_principal TEXT NOT NULL DEFAULT 'owner'`,
		`ALTER TABLE search_history ADD COLUMN IF NOT EXISTS owner_principal TEXT NOT NULL DEFAULT 'owner'`,
		`ALTER TABLE resurfacing_queue ADD COLUMN IF NOT EXISTS owner_principal TEXT NOT NULL DEFAULT 'owner'`,
	}},
	// The principal registry (ADR-079), mirroring SQLite migration 048:
	// principals, principal_identities, auth_tickets and the kv table
	// for API key records, seeded with the one owner principal. The
	// instance name lives in config, not the database, so the owner's
	// display name is "Owner". IF NOT EXISTS and ON CONFLICT DO NOTHING
	// make a replay a no-op.
	{Version: 28, Name: "principal registry", Statements: []string{
		`CREATE TABLE IF NOT EXISTS principals (
			id           TEXT PRIMARY KEY,
			kind         TEXT NOT NULL,
			display_name TEXT NOT NULL,
			roles        TEXT NOT NULL DEFAULT '["reader"]',
			status       TEXT NOT NULL DEFAULT 'active',
			created_at   TIMESTAMPTZ NOT NULL,
			revoked_at   TIMESTAMPTZ
		)`,
		`CREATE TABLE IF NOT EXISTS principal_identities (
			id           TEXT PRIMARY KEY,
			principal_id TEXT NOT NULL REFERENCES principals(id),
			method       TEXT NOT NULL,
			issuer       TEXT NOT NULL DEFAULT '',
			subject      TEXT NOT NULL,
			name         TEXT NOT NULL DEFAULT '',
			created_at   TIMESTAMPTZ NOT NULL,
			revoked_at   TIMESTAMPTZ,
			last_used_at TIMESTAMPTZ,
			UNIQUE (method, issuer, subject)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_principal_identities_principal ON principal_identities (principal_id)`,
		`CREATE TABLE IF NOT EXISTS auth_tickets (
			hash         TEXT PRIMARY KEY,
			kind         TEXT NOT NULL,
			principal_id TEXT REFERENCES principals(id),
			grants       TEXT NOT NULL DEFAULT '{}',
			created_by   TEXT NOT NULL REFERENCES principals(id),
			created_at   TIMESTAMPTZ NOT NULL,
			expires_at   TIMESTAMPTZ NOT NULL,
			redeemed_at  TIMESTAMPTZ
		)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_tickets_expires ON auth_tickets (expires_at)`,
		`CREATE TABLE IF NOT EXISTS kv (
			key        TEXT PRIMARY KEY,
			value      BYTEA NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL
		)`,
		`INSERT INTO principals (id, kind, display_name, roles, status, created_at)
		 VALUES ('owner', 'owner', 'Owner', '["admin"]', 'active', NOW())
		 ON CONFLICT (id) DO NOTHING`,
	}},
	// ui_sessions and ui_login_codes reference the identity that minted
	// them (ADR-079) instead of the hash of a static token, mirroring
	// SQLite migration 049. The rows, minted by static tokens that no
	// longer authenticate, are dropped before the column swap.
	{Version: 29, Name: "sessions bound to identities", fn: migrateSessionsBoundToIdentities},
	// The columns ObjectStore.List filters and orders by, mirroring SQLite
	// migration 050 (status has been indexed there since 009): the exact
	// COUNT(*) of the default scope can be an index-only walk of
	// idx_objects_status, and a page streams from its sort column's index
	// to the first rows instead of sorting every active object. Indexes
	// only; IF NOT EXISTS makes a replay a no-op.
	{Version: 30, Name: "object list indexes", Statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_objects_status ON objects (status)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_created_at ON objects (created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_objects_updated_at ON objects (updated_at)`,
	}},
	// The sharing registry (ADR-080), mirroring SQLite migration 051:
	// audiences and their members, share policies, grants and the read
	// audit; principals.deny_classes, principal_identities.share_policy_id;
	// objects.classes and the masked shadow projection
	// (projected_fts_body_masked with its generated fts_masked tsvector
	// and GIN index); embeddings.variant, extending the primary key. The
	// masked body is a projection output, so the projection version is
	// bumped with it (indexsig.ProjectionVersion v5) and the
	// re-projection fills it.
	{Version: 31, Name: "sharing registry", fn: migrateSharing},
	// metering_events, mirroring SQLite migration 019: the MeteringStore
	// the inbound entitlement gate charges every non-admin entity read
	// to. Postgres never had the table (its store was a placeholder), so
	// a gated read failed closed with a 500. New table and indexes only;
	// IF NOT EXISTS makes a replay a no-op.
	{Version: 32, Name: "metering_events", Statements: []string{
		`CREATE TABLE IF NOT EXISTS metering_events (
			id            TEXT PRIMARY KEY,
			registry_name TEXT NOT NULL,
			event_type    TEXT NOT NULL,
			namespace     TEXT NOT NULL DEFAULT '',
			count         INTEGER NOT NULL DEFAULT 1,
			occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_metering_events_registry_name ON metering_events (registry_name)`,
		`CREATE INDEX IF NOT EXISTS idx_metering_events_occurred_at ON metering_events (occurred_at)`,
		`CREATE INDEX IF NOT EXISTS idx_metering_events_registry_event ON metering_events (registry_name, event_type)`,
	}},
	// registry_entitlements.kind (outbound | inbound) keyed by (kind,
	// registry_name), mirroring SQLite migration 053: a remote
	// registry's synced entitlement and an operator's grant to a
	// principal of the same name are separate rows.
	{Version: 33, Name: "registry_entitlements.kind", fn: migrateEntitlementKind},
	// Content-hash dedup stays inside a profile (ADR-080 §4), mirroring
	// SQLite migration 052: a unique index on (content_hash, profile_id)
	// over non-empty hashes, so a second object with the hash in the
	// same profile is refused (storage.ErrAlreadyExists) and the dedup
	// callers reinforce. Postgres never had a unique hash index, so a
	// database may already hold such duplicates: the migration then
	// refuses, naming them, and changes nothing (see
	// migrateContentHashPerProfile). Index only, no re-projection.
	{Version: 34, Name: "objects (content_hash, profile_id) unique", fn: migrateContentHashPerProfile},
}

// sharingRegistryStatements create the sharing registry and add the
// sharing columns (migration 31). IF NOT EXISTS makes every statement
// re-runnable.
var sharingRegistryStatements = []string{
	`CREATE TABLE IF NOT EXISTS audiences (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL UNIQUE,
		created_at TIMESTAMPTZ NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS audience_members (
		audience_id  TEXT NOT NULL REFERENCES audiences(id) ON DELETE CASCADE,
		principal_id TEXT NOT NULL REFERENCES principals(id),
		expires_at   TIMESTAMPTZ,
		added_by     TEXT NOT NULL,
		added_at     TIMESTAMPTZ NOT NULL,
		PRIMARY KEY (audience_id, principal_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audience_members_principal ON audience_members (principal_id)`,
	`CREATE TABLE IF NOT EXISTS share_policies (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL UNIQUE,
		version    INTEGER NOT NULL,
		spec       JSONB NOT NULL,
		updated_at TIMESTAMPTZ NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS share_grants (
		id          TEXT PRIMARY KEY,
		audience_id TEXT NOT NULL REFERENCES audiences(id),
		policy_id   TEXT NOT NULL REFERENCES share_policies(id),
		expires_at  TIMESTAMPTZ,
		created_by  TEXT NOT NULL,
		created_at  TIMESTAMPTZ NOT NULL,
		revoked_at  TIMESTAMPTZ
	)`,
	`CREATE INDEX IF NOT EXISTS idx_share_grants_audience ON share_grants (audience_id)`,
	`CREATE TABLE IF NOT EXISTS access_log (
		id              TEXT PRIMARY KEY,
		at              TIMESTAMPTZ NOT NULL,
		principal_id    TEXT NOT NULL,
		identity_id     TEXT,
		route           TEXT NOT NULL,
		policy_versions TEXT NOT NULL,
		object_count    INTEGER NOT NULL,
		entity_count    INTEGER NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_access_log_principal ON access_log (principal_id, at)`,
	`CREATE INDEX IF NOT EXISTS idx_access_log_at ON access_log (at)`,
	`CREATE TABLE IF NOT EXISTS access_log_objects (
		access_id TEXT NOT NULL REFERENCES access_log(id) ON DELETE CASCADE,
		object_id TEXT NOT NULL,
		PRIMARY KEY (access_id, object_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_access_log_objects_object ON access_log_objects (object_id)`,
	`ALTER TABLE principals ADD COLUMN IF NOT EXISTS deny_classes TEXT NOT NULL DEFAULT '[]'`,
	`ALTER TABLE principal_identities ADD COLUMN IF NOT EXISTS share_policy_id TEXT REFERENCES share_policies(id)`,
	`ALTER TABLE objects ADD COLUMN IF NOT EXISTS classes JSONB NOT NULL DEFAULT '[]'::jsonb`,
	`ALTER TABLE objects ADD COLUMN IF NOT EXISTS projected_fts_body_masked TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE objects ADD COLUMN IF NOT EXISTS fts_masked tsvector
		GENERATED ALWAYS AS (to_tsvector('` + ftsRegconfig + `', projected_fts_body_masked)) STORED`,
	`CREATE INDEX IF NOT EXISTS idx_objects_fts_masked ON objects USING GIN (fts_masked)`,
}

// migrateSharing runs sharingRegistryStatements, then extends the
// embeddings primary key with variant: the column is added with every
// existing row raw, and the (object, model, chunk) key is replaced by
// (object, model, chunk, variant). The key swap happens only while the
// column is absent, so a replay is a no-op. The per-model partial HNSW
// indexes are keyed by model_id and unaffected.
func migrateSharing(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range sharingRegistryStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("create sharing registry: %w", err)
		}
	}
	var hasVariant bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'embeddings' AND column_name = 'variant'
		)`).Scan(&hasVariant); err != nil {
		return fmt.Errorf("inspect embeddings.variant: %w", err)
	}
	if !hasVariant {
		for _, stmt := range []string{
			`ALTER TABLE embeddings ADD COLUMN variant TEXT NOT NULL DEFAULT 'raw'
				CONSTRAINT embeddings_variant_known CHECK (variant IN ('raw', 'masked'))`,
			`ALTER TABLE embeddings DROP CONSTRAINT IF EXISTS embeddings_pkey`,
			`ALTER TABLE embeddings ADD CONSTRAINT embeddings_pkey PRIMARY KEY (object_id, model_id, chunk_idx, variant)`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("extend embeddings key with variant: %w", err)
			}
		}
	}
	return tx.Commit()
}

// migrateSessionsBoundToIdentities swaps token_hash for identity_id
// REFERENCES principal_identities(id) on ui_sessions and ui_login_codes.
// Every existing row was minted by a static token, which no longer
// authenticates, so both tables are emptied first; a NOT NULL column
// then adds cleanly. IF EXISTS and IF NOT EXISTS make a replay a no-op.
func migrateSessionsBoundToIdentities(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM ui_sessions WHERE EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'ui_sessions' AND column_name = 'token_hash')`,
		`DELETE FROM ui_login_codes WHERE EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_name = 'ui_login_codes' AND column_name = 'token_hash')`,
		`DROP INDEX IF EXISTS idx_ui_sessions_token_hash`,
		`ALTER TABLE ui_sessions DROP COLUMN IF EXISTS token_hash`,
		`ALTER TABLE ui_login_codes DROP COLUMN IF EXISTS token_hash`,
		`ALTER TABLE ui_sessions ADD COLUMN IF NOT EXISTS identity_id TEXT NOT NULL REFERENCES principal_identities(id)`,
		`ALTER TABLE ui_login_codes ADD COLUMN IF NOT EXISTS identity_id TEXT NOT NULL REFERENCES principal_identities(id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_sessions_identity ON ui_sessions (identity_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ui_login_codes_identity ON ui_login_codes (identity_id)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("sessions bound to identities: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// entitlementInboundShapeSQL matches a registry_entitlements row whose
// name is principal-shaped: the owner principal or a minted prn_ ID.
const entitlementInboundShapeSQL = `(registry_name = 'owner' OR left(registry_name, 4) = 'prn_')`

// migrateEntitlementKind adds registry_entitlements.kind and re-keys
// the table on (kind, registry_name), in one transaction. Backfill, as
// SQLite 053: a principal-shaped name (owner, prn_*) becomes one
// inbound row; any other name stays outbound and gains an inbound copy,
// because a static-token principal ("phone") cannot be told from a
// registry name and both readers used to see every row. A table that
// already has kind is left alone, so a replay is a no-op.
func migrateEntitlementKind(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var has bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		                WHERE table_schema = current_schema()
		                  AND table_name = 'registry_entitlements' AND column_name = 'kind')`,
	).Scan(&has); err != nil {
		return fmt.Errorf("inspect registry_entitlements.kind: %w", err)
	}
	if has {
		return nil
	}

	var pkey string
	if err := tx.QueryRowContext(ctx, `
		SELECT conname FROM pg_constraint
		 WHERE conrelid = 'registry_entitlements'::regclass AND contype = 'p'`,
	).Scan(&pkey); err != nil {
		return fmt.Errorf("inspect registry_entitlements primary key: %w", err)
	}

	const cols = `registry_name, plan, namespaces, expires_at, fetched_at`
	for _, stmt := range []string{
		`ALTER TABLE registry_entitlements ADD COLUMN kind TEXT NOT NULL DEFAULT 'outbound'`,
		`UPDATE registry_entitlements SET kind = 'inbound' WHERE ` + entitlementInboundShapeSQL,
		`ALTER TABLE registry_entitlements DROP CONSTRAINT "` + pkey + `"`,
		`INSERT INTO registry_entitlements (kind, ` + cols + `)
		 SELECT 'inbound', ` + cols + ` FROM registry_entitlements
		  WHERE kind = 'outbound' AND NOT ` + entitlementInboundShapeSQL,
		`ALTER TABLE registry_entitlements ALTER COLUMN kind DROP DEFAULT`,
		`ALTER TABLE registry_entitlements ADD CONSTRAINT registry_entitlements_kind_check
			CHECK (kind IN ('outbound', 'inbound'))`,
		`ALTER TABLE registry_entitlements ADD CONSTRAINT registry_entitlements_pkey
			PRIMARY KEY (kind, registry_name)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("re-key registry_entitlements: %w", err)
		}
	}
	return tx.Commit()
}

// contentHashDuplicatesShown is how many duplicate groups the error of
// migrateContentHashPerProfile names.
const contentHashDuplicatesShown = 5

// migrateContentHashPerProfile creates idx_objects_content_hash_profile
// (see the migrations entry). Objects that already share a non-empty
// hash in one profile would fail the index: rather than pick which of
// them to merge or drop, it returns an error naming them (the first
// groups and how many there are) and leaves the schema at 33, so the
// operator keeps one object per (content_hash, profile_id) and the next
// start applies it. The objects table is share-locked from the check to
// the index, so no write lands a duplicate in between. IF NOT EXISTS
// makes a replay a no-op.
func migrateContentHashPerProfile(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `LOCK TABLE objects IN SHARE MODE`); err != nil {
		return fmt.Errorf("lock objects: %w", err)
	}
	groups, total, err := contentHashDuplicates(ctx, tx)
	if err != nil {
		return fmt.Errorf("find duplicate content hashes: %w", err)
	}
	if total > 0 {
		shown := ""
		if total > len(groups) {
			shown = fmt.Sprintf("first %d: ", len(groups))
		}
		return fmt.Errorf("%d content hashes are each stored by more than one object of a profile (%s%s); "+
			"keep one object per (content_hash, profile_id): delete or merge the others, "+
			"or set their content_hash to '' to take them out of dedup, then start again",
			total, shown, strings.Join(groups, "; "))
	}

	if _, err := tx.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS idx_objects_content_hash_profile
		ON objects (content_hash, profile_id) WHERE content_hash <> ''`); err != nil {
		return fmt.Errorf("create content hash index: %w", err)
	}
	return tx.Commit()
}

// contentHashDuplicates describes the first contentHashDuplicatesShown
// groups of objects sharing a non-empty hash in one profile, oldest
// object first, and counts every such group.
func contentHashDuplicates(ctx context.Context, tx *sql.Tx) ([]string, int, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT profile_id, content_hash, string_agg(id, ', ' ORDER BY created_at, id), COUNT(*) OVER ()
		  FROM objects
		 WHERE content_hash <> ''
		 GROUP BY profile_id, content_hash
		HAVING COUNT(*) > 1
		 ORDER BY profile_id, content_hash
		 LIMIT $1`, contentHashDuplicatesShown)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var groups []string
	total := 0
	for rows.Next() {
		var profile, hash, ids string
		if err := rows.Scan(&profile, &hash, &ids, &total); err != nil {
			return nil, 0, err
		}
		groups = append(groups, fmt.Sprintf("profile %q hash %q: %s", profile, hash, ids))
	}
	return groups, total, rows.Err()
}

// migrateAliasesObjectFK deletes aliases whose object is gone, then adds
// aliases_object_fk (ON DELETE CASCADE), guarded by a pg_constraint
// lookup so a replay is a no-op. One transaction: the constraint never
// lands without the cleanup it needs.
func migrateAliasesObjectFK(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM aliases a
		 WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.id = a.object_id)`); err != nil {
		return fmt.Errorf("delete orphaned aliases: %w", err)
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM pg_constraint
		                WHERE conname = 'aliases_object_fk' AND conrelid = 'aliases'::regclass)`).Scan(&exists); err != nil {
		return fmt.Errorf("inspect aliases_object_fk: %w", err)
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE aliases ADD CONSTRAINT aliases_object_fk
			FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE`); err != nil {
			return fmt.Errorf("add aliases_object_fk: %w", err)
		}
	}
	return tx.Commit()
}

// migrateProximityObjectFK deletes object_proximity pairs with a missing
// object on either side, then adds object_proximity_a_fk and
// object_proximity_b_fk (ON DELETE CASCADE), each guarded by a
// pg_constraint lookup so a replay is a no-op. One transaction, like
// migrateAliasesObjectFK.
func migrateProximityObjectFK(ctx context.Context, d *Driver) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM object_proximity p
		 WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.id = p.object_a)
		    OR NOT EXISTS (SELECT 1 FROM objects o WHERE o.id = p.object_b)`); err != nil {
		return fmt.Errorf("delete orphaned proximity pairs: %w", err)
	}
	for _, fk := range []struct{ name, column string }{
		{"object_proximity_a_fk", "object_a"},
		{"object_proximity_b_fk", "object_b"},
	} {
		var exists bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM pg_constraint
			                WHERE conname = $1 AND conrelid = 'object_proximity'::regclass)`,
			fk.name).Scan(&exists); err != nil {
			return fmt.Errorf("inspect %s: %w", fk.name, err)
		}
		if exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, `ALTER TABLE object_proximity ADD CONSTRAINT `+fk.name+`
			FOREIGN KEY (`+fk.column+`) REFERENCES objects(id) ON DELETE CASCADE`); err != nil {
			return fmt.Errorf("add %s: %w", fk.name, err)
		}
	}
	return tx.Commit()
}

// objectChangeSeqStatements install objects.change_seq (migration 22).
// The trigger takes the next value from the single object_change_counter
// row, whose row lock the writing transaction then holds until it
// commits: concurrent object writes serialize on it, so change_seq
// values commit in increasing order and a reader never sees a value
// while a smaller one is still uncommitted. A SEQUENCE would not hold
// that — its values commit out of order, and a watermark could pass a
// change still in flight. Every statement is re-runnable: the backfill
// numbers only unstamped rows, after the largest value already there.
var objectChangeSeqStatements = []string{
	`ALTER TABLE objects ADD COLUMN IF NOT EXISTS change_seq BIGINT NOT NULL DEFAULT 0`,
	`DROP TRIGGER IF EXISTS objects_change_seq ON objects`,
	`UPDATE objects o SET change_seq = r.base + r.rn
	   FROM (SELECT id,
	                row_number() OVER (ORDER BY updated_at, id COLLATE "C") AS rn,
	                (SELECT COALESCE(MAX(change_seq), 0) FROM objects) AS base
	           FROM objects WHERE change_seq = 0) r
	  WHERE o.id = r.id`,
	`CREATE TABLE IF NOT EXISTS object_change_counter (
		id  SMALLINT PRIMARY KEY CHECK (id = 1),
		seq BIGINT NOT NULL
	)`,
	`INSERT INTO object_change_counter (id, seq)
	 SELECT 1, COALESCE(MAX(change_seq), 0) FROM objects
	 ON CONFLICT (id) DO UPDATE SET seq = GREATEST(object_change_counter.seq, EXCLUDED.seq)`,
	`CREATE OR REPLACE FUNCTION objects_stamp_change_seq() RETURNS trigger
	 LANGUAGE plpgsql AS $$
	 BEGIN
		UPDATE object_change_counter SET seq = seq + 1 WHERE id = 1 RETURNING seq INTO NEW.change_seq;
		RETURN NEW;
	 END
	 $$`,
	`CREATE TRIGGER objects_change_seq BEFORE INSERT OR UPDATE ON objects
	 FOR EACH ROW EXECUTE FUNCTION objects_stamp_change_seq()`,
	`CREATE INDEX IF NOT EXISTS idx_objects_change_seq ON objects (change_seq)`,
	`DROP INDEX IF EXISTS idx_objects_change_order`,
	`ALTER TABLE federation_watermarks ADD COLUMN IF NOT EXISTS last_change_seq BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE federation_watermarks DROP COLUMN IF EXISTS last_object_id`,
	`ALTER TABLE federation_watermarks DROP COLUMN IF EXISTS last_synced_at`,
}

// migrateProjectionVersion adds objects.projection_version, the projection
// version (indexsig.ProjectionVersion) each row's projected_fts_body was
// derived under, and clears the stored FTS signature hash: unstamped rows
// may hold any earlier projection, so the signature no longer vouches for
// them. Both happen only when the column is added, in one transaction.
func migrateProjectionVersion(ctx context.Context, d *Driver) error {
	var exists bool
	if err := d.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'objects' AND column_name = 'projection_version'
		)`).Scan(&exists); err != nil {
		return fmt.Errorf("check projection_version column: %w", err)
	}
	if exists {
		return nil
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
		SET signature_hash = '', inputs_summary = $1
		WHERE signature_id = $2`,
		"cleared by migration 18: rows carry no projection_version stamp", indexsig.FTSSignatureID); err != nil {
		return fmt.Errorf("clear fts signature: %w", err)
	}
	return tx.Commit()
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

// pgPendingTarget returns the highest migration version above current
// and at most target, or 0 when no migration is pending.
func pgPendingTarget(current, target int) int {
	to := 0
	for _, m := range pgMigrations {
		if m.Version > current && m.Version <= target && m.Version > to {
			to = m.Version
		}
	}
	return to
}

// pgRowQuerier is the QueryRowContext of *sql.DB and *sql.Tx.
type pgRowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// pgHasNoOwnTables reports whether the current schema holds no tables
// besides the migration ledger: a version-0 database with nothing to
// lose. A version-0 database with tables (initialized before the
// ledger existed) is an existing one. SQLite applies the same rule.
func pgHasNoOwnTables(ctx context.Context, q pgRowQuerier) (bool, error) {
	var empty bool
	if err := q.QueryRowContext(ctx, `SELECT NOT EXISTS (
		SELECT 1 FROM pg_catalog.pg_tables
		 WHERE schemaname = current_schema() AND tablename <> 'schema_version')`).Scan(&empty); err != nil {
		return false, fmt.Errorf("list tables: %w", err)
	}
	return empty, nil
}

// warnUnsnapshottedMigration warns before migrations run against an
// existing Postgres database. Unlike SQLite, Postgres gets no automatic
// snapshot: backing it up is the operator's job (pg_dump), so the
// warning names both versions and the command. A brand-new database
// (version 0, empty) has nothing to lose and gets no warning.
func warnUnsnapshottedMigration(current, target int, empty bool) {
	to := pgPendingTarget(current, target)
	if to == 0 || (current == 0 && empty) {
		return
	}
	slog.Warn(fmt.Sprintf("migrating Postgres schema v%d to v%d without a snapshot: dpkms never backs up Postgres; take a pg_dump before upgrading", current, to),
		"from", current, "to", to)
}

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
	// Close only hands the session back to the pool (or drops a poisoned
	// one); there is no work left whose failure it could report.
	defer func() { _ = conn.Close() }()
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
	empty := false
	if current == 0 {
		if empty, err = pgHasNoOwnTables(ctx, d.db); err != nil {
			return fmt.Errorf("inspect unversioned database: %w", err)
		}
	}
	warnUnsnapshottedMigration(current, target, empty)

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
	// ADR-071 Phase 1: embedding_models registry +
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
// Mirrors migrateJobsUserMentions exactly.
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
// migrateJobsUserMentions / migrateJobsUserHints.
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
	// scope, when set, is the restricted viewer's visibility every read
	// ANDs into its SQL (see scope.go); nil is the driver's own store.
	scope *storage.Scope
}
type (
	EntityStore struct {
		db *sql.DB
		// scope, when set, keeps every read to the entities the viewer
		// may see (see scope.go).
		scope *storage.Scope
	}
	EdgeStore struct {
		db *sql.DB
		// scope, when set, keeps every read to the edges whose object
		// ends the viewer may see (see scope.go).
		scope *storage.Scope
	}
	JobStore      struct{ db *sql.DB }
	PipelineStore struct{ db *sql.DB }
	StepStore     struct{ db *sql.DB }
	RegistryStore struct{ db *sql.DB }
	ReminderStore struct{ db *sql.DB }
	FeedStore     struct{ db *sql.DB }
	FeedItemStore struct{ db *sql.DB }
	BatchStore    struct{ db *sql.DB }
)
