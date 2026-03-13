-- Embedding storage using BLOB for vector data
-- Cosine similarity is computed in application code

CREATE TABLE IF NOT EXISTS object_embeddings (
    id          TEXT PRIMARY KEY,
    embedding   BLOB NOT NULL,
    dimensions  INTEGER NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (id) REFERENCES objects(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_object_embeddings_id ON object_embeddings(id);
