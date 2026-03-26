-- T-0130: resurfacing queue for active profile
CREATE TABLE IF NOT EXISTS resurfacing_queue (
    id           TEXT PRIMARY KEY,
    object_id    TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    profile_id   TEXT NOT NULL,
    score        REAL NOT NULL,
    reason       TEXT NOT NULL,
    surfaced_at  TEXT,
    dismissed_at TEXT,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_profile_id
    ON resurfacing_queue (profile_id);

CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_object_id
    ON resurfacing_queue (object_id);

CREATE INDEX IF NOT EXISTS idx_resurfacing_queue_unseen
    ON resurfacing_queue (profile_id, score)
    WHERE surfaced_at IS NULL AND dismissed_at IS NULL;
