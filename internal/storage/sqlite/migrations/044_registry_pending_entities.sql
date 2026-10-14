-- Migration 044: approval queue for entities untrusted registries serve.
--
-- Registry sync writes a trusted registry's entities and holds an
-- untrusted one's here until `ctxt registry approve` writes them or
-- `ctxt registry reject` drops them. One row per registry, slug and
-- version (version_hash, or a content hash when the registry serves
-- none); id is derived from the three. Times are RFC 3339 UTC.
CREATE TABLE IF NOT EXISTS registry_pending_entities (
    id               TEXT PRIMARY KEY,
    registry_name    TEXT NOT NULL,
    registry_url     TEXT NOT NULL,
    slug             TEXT NOT NULL,
    namespace        TEXT NOT NULL DEFAULT '',
    title            TEXT NOT NULL DEFAULT '',
    version          TEXT NOT NULL,
    payload          TEXT NOT NULL,
    signature_status TEXT NOT NULL,
    first_seen_at    TEXT NOT NULL,
    last_seen_at     TEXT NOT NULL,
    UNIQUE (registry_name, slug, version)
);
