-- Migration 013: thin sync support for entities
-- Adds content_status, version_hash, registry_url columns.
-- content_status: 'full' | 'thin' | 'pending_pull' (default 'full').
ALTER TABLE entities ADD COLUMN content_status TEXT NOT NULL DEFAULT 'full';
ALTER TABLE entities ADD COLUMN version_hash   TEXT NOT NULL DEFAULT '';
ALTER TABLE entities ADD COLUMN registry_url   TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_entities_content_status ON entities(content_status);
CREATE INDEX IF NOT EXISTS idx_entities_registry_url   ON entities(registry_url);
