-- Migration 032: embedding_models registry + embeddings composite-key table (T-0582, ADR-071 Phase 1).
--
-- ADR-071 §"Data model" mandates:
--   - embedding_models: one row per registered model (model_id PK).
--   - embeddings: composite key (object_id, model_id, chunk_idx).
--   - idx_embeddings_model on (model_id, object_id).
--   - partial unique index enforcing at most one is_default = 1 row.
--
-- The legacy `object_embeddings` rows are migrated into `embeddings` under a
-- synthetic model_id by migrate033EmbeddingsBackfill (the next migration,
-- which runs as a Go fn so the synthetic model_id can be derived from the
-- driver's vectorDimension and the SQL is conditional on the legacy table
-- existing).
CREATE TABLE IF NOT EXISTS embedding_models (
    model_id      TEXT NOT NULL PRIMARY KEY,
    provider      TEXT NOT NULL DEFAULT '',
    dimension     INTEGER NOT NULL DEFAULT 0,
    is_default    INTEGER NOT NULL DEFAULT 0,
    registered_at TEXT NOT NULL,
    deprecated_at TEXT,
    config_json   TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS embeddings (
    object_id  TEXT NOT NULL,
    model_id   TEXT NOT NULL REFERENCES embedding_models(model_id),
    chunk_idx  INTEGER NOT NULL DEFAULT 0,
    vector     BLOB NOT NULL,
    text       TEXT,
    created_at TEXT NOT NULL,
    PRIMARY KEY (object_id, model_id, chunk_idx)
);

CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings (model_id, object_id);

-- ADR-071 §"Data model": at most one row may be is_default=1. Partial unique
-- index lets many rows with is_default=0 coexist while enforcing the
-- singleton on is_default=1.
CREATE UNIQUE INDEX IF NOT EXISTS idx_embedding_default
    ON embedding_models(is_default) WHERE is_default = 1;
