-- Knowledge objects
CREATE TABLE IF NOT EXISTS objects (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    subtype TEXT DEFAULT '',
    raw_content TEXT DEFAULT '',
    content_type TEXT DEFAULT '',
    metadata JSON DEFAULT '{}',
    summaries JSON DEFAULT '[]',
    sections JSON DEFAULT '[]',
    tags JSON DEFAULT '[]',
    mentions JSON DEFAULT '[]',
    decisions JSON DEFAULT '[]',
    tasks JSON DEFAULT '[]',
    embeddings BLOB,
    pipeline TEXT DEFAULT '',
    source TEXT DEFAULT '',
    registry_influences JSON DEFAULT '[]',
    plugins JSON DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    fts_indexed INTEGER DEFAULT 0,
    vector_indexed INTEGER DEFAULT 0
);

-- Full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS objects_fts USING fts5(
    id UNINDEXED,
    summaries,
    raw_content,
    content='objects',
    content_rowid='rowid'
);

-- Entities
CREATE TABLE IF NOT EXISTS entities (
    slug TEXT PRIMARY KEY,
    title TEXT DEFAULT '',
    description TEXT DEFAULT '',
    namespace TEXT DEFAULT '',
    aliases JSON DEFAULT '[]',
    metadata JSON DEFAULT '{}',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Edges (source of truth for all relationships — ADR-049)
CREATE TABLE IF NOT EXISTS edges (
    id TEXT PRIMARY KEY,
    from_type TEXT NOT NULL,
    from_id TEXT NOT NULL,
    to_type TEXT NOT NULL,
    to_id TEXT NOT NULL,
    edge_type TEXT NOT NULL,
    weight REAL DEFAULT 1.0,
    metadata JSON DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_type, from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to ON edges(to_type, to_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(edge_type);
CREATE INDEX IF NOT EXISTS idx_edges_from_type ON edges(from_type, from_id, edge_type);

-- Jobs
CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    payload TEXT DEFAULT '',
    pipeline TEXT DEFAULT '',
    source TEXT DEFAULT '',
    result_id TEXT DEFAULT '',
    error TEXT DEFAULT '',
    retry_count INTEGER DEFAULT 0,
    max_retries INTEGER DEFAULT 3,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs(type);

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
