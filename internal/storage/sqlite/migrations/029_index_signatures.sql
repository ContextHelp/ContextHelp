-- Migration 028: index_signatures table (T-0579, ADR-070 §3).
--
-- Stores per-table signature stamps so the daemon can detect on startup
-- that the on-disk index shape no longer matches the build-time-known
-- shape (tokenizer + projection logic + FTS schema DDL). Mismatch is
-- the bucket-1 (`reindex_auto`) trigger.
--
-- This migration only ships detection scaffolding; the actual reindex
-- worker is T-0581.
CREATE TABLE IF NOT EXISTS index_signatures (
    signature_id   TEXT NOT NULL PRIMARY KEY, -- e.g. "objects_fts", "embeddings_<model_id>"
    signature_hash TEXT NOT NULL,             -- hex sha256 of relevant inputs
    computed_at    TEXT NOT NULL,             -- ISO 8601 UTC
    inputs_summary TEXT NOT NULL DEFAULT ''   -- human-readable: "tokenizer=fts5-default;projection=v1"
);
