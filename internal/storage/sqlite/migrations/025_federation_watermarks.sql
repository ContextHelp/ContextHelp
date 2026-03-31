CREATE TABLE IF NOT EXISTS federation_watermarks (
    federation_name TEXT PRIMARY KEY,
    last_synced_at  TEXT NOT NULL DEFAULT ''
);
