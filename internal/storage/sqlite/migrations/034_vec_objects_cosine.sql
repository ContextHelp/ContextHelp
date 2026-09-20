-- ANN vector index via sqlite-vec (vec0 virtual table), recreated with an
-- explicit cosine distance metric. Cosine is the pinned cross-driver
-- distance contract (pgvector ranks by cosine <=>); the previous table used
-- vec0's implicit L2 default, which nobody chose deliberately and which
-- ranks differently for non-unit-norm embeddings.
--
-- vec_objects is index-only data: it is rebuilt from object_embeddings by
-- the migration function that executes this DDL.
-- The placeholder {DIMENSION} is replaced by the Driver before execution.
CREATE VIRTUAL TABLE IF NOT EXISTS vec_objects USING vec0(
    id TEXT PRIMARY KEY,
    embedding float[{DIMENSION}] distance_metric=cosine
);
