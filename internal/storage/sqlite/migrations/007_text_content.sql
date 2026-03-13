-- Add text_content column for searchable text extracted from binary blobs.
ALTER TABLE objects ADD COLUMN text_content TEXT DEFAULT '';

-- Rebuild FTS to include text_content.
DROP TABLE IF EXISTS objects_fts;
CREATE VIRTUAL TABLE objects_fts USING fts5(
    id UNINDEXED,
    summaries,
    raw_content,
    text_content,
    content='objects',
    content_rowid='rowid'
);
