-- Object proximity scores (symmetric; store only object_a < object_b).
CREATE TABLE IF NOT EXISTS object_proximity (
    object_a    TEXT NOT NULL,
    object_b    TEXT NOT NULL,
    score       REAL NOT NULL,
    semantic    REAL NOT NULL DEFAULT 0,
    temporal    REAL NOT NULL DEFAULT 0,
    entity      REAL NOT NULL DEFAULT 0,
    origin      REAL NOT NULL DEFAULT 0,
    behavioral  REAL NOT NULL DEFAULT 0,
    computed_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),

    PRIMARY KEY (object_a, object_b),
    CHECK (object_a < object_b)
);

-- Fast lookup of nearest neighbours for a given object.
CREATE INDEX IF NOT EXISTS idx_proximity_a_score ON object_proximity(object_a, score DESC);
CREATE INDEX IF NOT EXISTS idx_proximity_b_score ON object_proximity(object_b, score DESC);

-- Global score filter (e.g. "give me all high-proximity pairs").
CREATE INDEX IF NOT EXISTS idx_proximity_score ON object_proximity(score DESC);

-- Staleness detection: find pairs that need recomputation.
CREATE INDEX IF NOT EXISTS idx_proximity_computed ON object_proximity(computed_at);
