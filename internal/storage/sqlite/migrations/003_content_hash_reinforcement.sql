-- Content hash (SHA-256 of normalized raw_content + source)
ALTER TABLE objects ADD COLUMN content_hash TEXT DEFAULT '';

-- Reinforcement count (times content was seen/reinforced)
ALTER TABLE objects ADD COLUMN reinforcement_count INTEGER DEFAULT 1;

-- Last reinforced timestamp
ALTER TABLE objects ADD COLUMN last_reinforced_at TEXT;

-- Unique index for fast duplicate lookup (partial: excludes pre-migration empty hashes)
CREATE UNIQUE INDEX IF NOT EXISTS idx_objects_content_hash ON objects(content_hash) WHERE content_hash != '';
