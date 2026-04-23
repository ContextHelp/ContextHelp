-- External dedup key (Slack ts, tweet ID, email message-id, etc.)
ALTER TABLE objects ADD COLUMN source_key TEXT DEFAULT '';

-- Unique index for fast source-key dedup lookup (excludes empty keys)
CREATE UNIQUE INDEX IF NOT EXISTS idx_objects_source_key ON objects(source_key) WHERE source_key != '';
