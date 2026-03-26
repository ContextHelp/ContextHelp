-- ANN vector index via sqlite-vec (vec0 virtual table).
-- Dimension is set at Driver construction time (default 1536).
-- The placeholder {DIMENSION} is replaced by the Driver before execution.
CREATE VIRTUAL TABLE IF NOT EXISTS vec_objects USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[{DIMENSION}]
);
