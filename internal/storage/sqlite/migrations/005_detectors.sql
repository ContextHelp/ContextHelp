CREATE TABLE IF NOT EXISTS detectors (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,
    name         TEXT NOT NULL,
    pipeline_name TEXT NOT NULL,
    pattern      TEXT NOT NULL DEFAULT '',
    priority     INTEGER NOT NULL DEFAULT 100,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_detectors_kind     ON detectors(kind);
CREATE INDEX IF NOT EXISTS idx_detectors_enabled  ON detectors(enabled);
CREATE INDEX IF NOT EXISTS idx_detectors_priority ON detectors(priority);
