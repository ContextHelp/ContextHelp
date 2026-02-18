CREATE TABLE IF NOT EXISTS feeds (
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
    last_sync TEXT,
    error_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS feed_items (
    id TEXT PRIMARY KEY,
    feed_id TEXT NOT NULL REFERENCES feeds(id),
    guid TEXT NOT NULL,
    link TEXT DEFAULT '',
    object_id TEXT DEFAULT '',
    ingested_at TEXT NOT NULL,
    UNIQUE(feed_id, guid)
);

CREATE TABLE IF NOT EXISTS batches (
    id TEXT PRIMARY KEY,
    format TEXT NOT NULL,
    total_records INTEGER NOT NULL DEFAULT 0,
    completed INTEGER NOT NULL DEFAULT 0,
    failed INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'processing',
    errors JSON DEFAULT '[]',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feeds_status ON feeds(status);
CREATE INDEX IF NOT EXISTS idx_feed_items_feed_id ON feed_items(feed_id);
CREATE INDEX IF NOT EXISTS idx_feed_items_guid ON feed_items(feed_id, guid);
CREATE INDEX IF NOT EXISTS idx_batches_status ON batches(status);
