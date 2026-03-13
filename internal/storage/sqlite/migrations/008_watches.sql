CREATE TABLE IF NOT EXISTS watches (
    id               TEXT PRIMARY KEY,
    path             TEXT NOT NULL UNIQUE,
    mode             TEXT NOT NULL DEFAULT 'generic',
    include_patterns JSON DEFAULT '[]',
    exclude_patterns JSON DEFAULT '[]',
    debounce_ms      INTEGER NOT NULL DEFAULT 500,
    status           TEXT NOT NULL DEFAULT 'active',
    pipeline_override TEXT DEFAULT '',
    last_error       TEXT DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS watch_file_records (
    watch_id     TEXT NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
    file_path    TEXT NOT NULL,
    object_id    TEXT DEFAULT '',
    content_hash TEXT DEFAULT '',
    last_seen    TEXT NOT NULL,
    PRIMARY KEY (watch_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_watches_status ON watches(status);
CREATE INDEX IF NOT EXISTS idx_wfr_watch_id   ON watch_file_records(watch_id);
