-- T-0126: attachment store for binary content linked to knowledge objects
CREATE TABLE attachments (
    id          TEXT PRIMARY KEY,
    object_id   TEXT NOT NULL REFERENCES objects(id),
    filename    TEXT NOT NULL,
    mime_type   TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL,
    data        BLOB NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_attachments_object_id ON attachments(object_id);
