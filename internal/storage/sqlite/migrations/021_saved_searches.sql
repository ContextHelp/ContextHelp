-- T-0160: saved_searches and search_history tables (US-0054, US-0055)
CREATE TABLE IF NOT EXISTS saved_searches (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    query        TEXT NOT NULL,
    profile_id   TEXT NOT NULL DEFAULT '',
    alert_on     TEXT NOT NULL DEFAULT '',
    notify       TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_saved_searches_name
    ON saved_searches (name);

CREATE TABLE IF NOT EXISTS search_history (
    id               TEXT PRIMARY KEY,
    query            TEXT NOT NULL,
    profile_id       TEXT NOT NULL DEFAULT '',
    strategies_used  TEXT NOT NULL DEFAULT '',
    result_count     INTEGER NOT NULL DEFAULT 0,
    searched_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_search_history_profile_id
    ON search_history (profile_id);

CREATE INDEX IF NOT EXISTS idx_search_history_searched_at
    ON search_history (searched_at DESC);
