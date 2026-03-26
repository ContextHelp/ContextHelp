-- T-0140: metering events for paid registry access tracking
CREATE TABLE IF NOT EXISTS metering_events (
    id            TEXT PRIMARY KEY,
    registry_name TEXT NOT NULL,
    event_type    TEXT NOT NULL,   -- entity_resolve | content_pull | taxonomy_sync
    namespace     TEXT,
    count         INTEGER NOT NULL DEFAULT 1,
    occurred_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_metering_events_registry_name
    ON metering_events (registry_name);

CREATE INDEX IF NOT EXISTS idx_metering_events_occurred_at
    ON metering_events (occurred_at);

CREATE INDEX IF NOT EXISTS idx_metering_events_registry_event
    ON metering_events (registry_name, event_type);
