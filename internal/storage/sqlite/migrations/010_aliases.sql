CREATE TABLE IF NOT EXISTS aliases (
    alias       TEXT NOT NULL,
    object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    scope       TEXT NOT NULL DEFAULT 'global',
    profile     TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    PRIMARY KEY (alias, scope, profile)
);

CREATE INDEX IF NOT EXISTS idx_aliases_object_id ON aliases(object_id);
CREATE INDEX IF NOT EXISTS idx_aliases_scope_profile ON aliases(scope, profile);
