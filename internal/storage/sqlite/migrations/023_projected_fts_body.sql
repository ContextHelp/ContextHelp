-- Migration 023: projected_fts_body column + FTS rebuild (T-0195).
--
-- projected_fts_body holds the output of projection.ProjectIndex(ko).FTSBody
-- so that FTS5 indexes graph-canonical text rather than raw columns.
-- The FTS virtual table is rebuilt to include this column as the indexed body.
-- Backfill sets projected_fts_body = '' for existing rows; the enrichment
-- pipeline re-populates on next Create/Update.
ALTER TABLE objects ADD COLUMN projected_fts_body TEXT NOT NULL DEFAULT '';

-- Rebuild FTS to index projected_fts_body instead of raw summaries/raw_content.
DROP TABLE IF EXISTS objects_fts;
CREATE VIRTUAL TABLE objects_fts USING fts5(
    id UNINDEXED,
    projected_fts_body,
    content='objects',
    content_rowid='rowid'
);
