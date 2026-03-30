-- Migration 022: graph-canonical storage columns for KnowledgeObject (ADR-063).

-- graph_json stores the full ObjectGraph (typed nodes + edges) as JSON.
-- NULL for legacy rows; backfill in the Go fn sets '{}' for existing rows.
ALTER TABLE objects ADD COLUMN graph_json TEXT DEFAULT NULL;

-- object_nodes: denormalised index of node entries for node-type queries.
CREATE TABLE IF NOT EXISTS object_nodes (
    id          TEXT PRIMARY KEY,
    object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
    node_type   TEXT NOT NULL,
    ordinal     INTEGER NOT NULL DEFAULT 0,
    content     TEXT DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_object_nodes_object_id ON object_nodes(object_id);
CREATE INDEX IF NOT EXISTS idx_object_nodes_node_type ON object_nodes(node_type);
CREATE INDEX IF NOT EXISTS idx_object_nodes_object_node_type ON object_nodes(object_id, node_type);
