-- sqlite-vec virtual table for vector similarity search
-- Requires sqlite-vec extension to be loaded

CREATE VIRTUAL TABLE IF NOT EXISTS objects_vec USING vec0(
    id TEXT PRIMARY KEY,
    embedding FLOAT[1536]  -- OpenAI ada-002 dimension; adjust as needed
);

-- Index for faster vector lookups by id
CREATE INDEX IF NOT EXISTS idx_objects_vec_id ON objects_vec(id);
