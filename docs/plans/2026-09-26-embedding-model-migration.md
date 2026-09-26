# Embedding model migration: per-model index, single write path

> **Date:** 2026-09-26
> **Decision record:** [ADR-071, Amendment 2026-09-26](../decisions/ADR-071-embedding-index-versioning.md#amendment-2026-09-26)
> **Audience:** implementers of the four parallel tasks below

This plan implements the ADR-071 amendment: one write path (`storage.EmbeddingStore`), one ANN index per registered model at the registry's measured dimension, a query path that reads the default model, and removal of the pre-registry single-vector path and the `legacy-blob` placeholder. There is no backward compatibility. All migrations are forward-only.

The work splits into four tasks that run in parallel on disjoint files, plus a final sweep:

| Task | Delivers |
|---|---|
| **Register probe** | `ctxt embeddings register` measures the dimension from the provider, stores the provider config, and builds the index |
| **Per-model index** | schema migrations, both `EmbeddingStore` implementations, per-model signatures |
| **Ingest writes** | the embedding step produces per-model vectors, and persistence calls `Put` |
| **Query path** | `find` and hybrid search read the default model's index, with a visible FTS notice when they cannot |
| **Legacy sweep** (after ingest + query merge) | drops the old tables, columns, fields and driver plumbing |

The provider resolver is being built separately. It lands before the register-probe, ingest-writes and query-path tasks, and they consume it only through `embeddings.ProviderResolver`.

## Contract (already landed with this plan)

The contract commit that ships with this document declares the shared types and interfaces. Every task builds on it and none of them edits these files.

| File | Declares |
|---|---|
| `internal/storage/embeddings.go` | `EmbeddingStore`, `EmbeddingModelSpec`, `VectorQuery`, `EmbeddingHit`, `ObjectVector` (alias), `ErrEmbeddingIndexMissing`, `ErrEmbeddingDimension`, `SQLiteVecMaxDimension = 8192`, `ValidateEmbeddingModelID`, `EmbeddingIndexName` |
| `internal/storage/storage.go` | `StorageDriver.Embeddings() EmbeddingStore` |
| `pkg/pluginapi/pluginapi.go` | `ObjectVector{ModelID, ChunkIdx, Vector, Text}`; `KnowledgeObject.Vectors []ObjectVector` (`json:"-"`) |
| `internal/storage/{sqlite,postgres}/embeddings.go` | stub `EmbeddingStore`: every method returns `errors.ErrUnsupported`. **Per-model index replaces the stubs.** |
| `internal/embeddings/embeddings.go` | `ModelSource` (implemented by `*registry.Store`), `ProviderResolver`, `SpecFor(registry.Model)` |
| `internal/embeddings/registry/active.go` | `Store.Default` (`ErrNoDefaultModel`), `Store.Populating(now)`, `ForDriver(driver)` |
| test fakes | `Embeddings()` on the `StorageDriver` fakes in `internal/storage`, `internal/retrieval`, `internal/steps` |

### `EmbeddingStore` semantics (binding for every implementation)

```go
type EmbeddingStore interface {
    EnsureIndex(ctx, spec EmbeddingModelSpec) error
    Put(ctx, objectID string, vectors []ObjectVector) error
    Get(ctx, objectID, modelID string) ([]ObjectVector, error)
    Search(ctx, q VectorQuery) ([]EmbeddingHit, error)
    ListMissing(ctx, modelID, afterObjectID string, limit int) ([]string, error)
    PurgeModel(ctx, modelID string) error
}
```

- **`EnsureIndex`**
  - Idempotent.
  - Creates the index when absent. When the stored `embeddings_<model_id>` signature differs from the one computed from the *desired* DDL, it rebuilds the index from canonical rows in one transaction, then stamps the signature.
  - Returns `ErrEmbeddingDimension` when the dimension is ≤ 0 or above the backend ceiling (8192 on SQLite, 2000 on Postgres).
  - Validates the model ID with `ValidateEmbeddingModelID`.
- **`Put`**
  - For each model present in `vectors`, replaces all of that object's rows under that model. Other models' rows are untouched.
  - Returns `ErrEmbeddingDimension` when a vector's length is not the registry dimension. The check reads `embedding_models.dimension` and may cache it per store.
  - Skips zero-magnitude vectors.
  - Returns `ErrEmbeddingIndexMissing` when a model has no index.
  - Canonical rows and index entries commit atomically.
- **`Search`**
  - Returns cosine distance, ascending.
  - Collapses chunks to the best chunk per object: over-fetch, then collapse in Go.
  - Returns `ErrEmbeddingIndexMissing` for a model without an index.
  - `TopK <= 0` means 10.
- **`ListMissing`**
  - Returns IDs from `objects` that have no row under `modelID`, `id > afterObjectID`, `ORDER BY id`, capped at `limit`.
- **`PurgeModel`**
  - Deletes the model's rows, its index and its signature row. The registry row is not touched.

## Schema migrations

The current highest versions are **SQLite 36** (`internal/storage/sqlite/migrations.go`) and **Postgres 13** (`internal/storage/postgres/migrations.go`).

### SQLite 037 — `per-model embeddings` (per-model index task)

This is a Go function, `migrate037PerModelEmbeddings`. Steps 1 and 2 run in one transaction. Step 1 is skipped when `pragma_table_info('embeddings')` already has an `id` column.

1. Rebuild `embeddings` with a stable rowid alias for vec0 linkage, a foreign key to `objects`, and no placeholder rows:

   ```sql
   CREATE TABLE embeddings_v2 (
       id          INTEGER PRIMARY KEY,
       object_id   TEXT NOT NULL REFERENCES objects(id) ON DELETE CASCADE,
       model_id    TEXT NOT NULL REFERENCES embedding_models(model_id),
       chunk_idx   INTEGER NOT NULL DEFAULT 0 CHECK (chunk_idx >= 0),
       vector      BLOB NOT NULL,
       text        TEXT,
       created_at  TEXT NOT NULL,
       UNIQUE (object_id, model_id, chunk_idx)
   );
   INSERT INTO embeddings_v2 (object_id, model_id, chunk_idx, vector, text, created_at)
       SELECT e.object_id, e.model_id, e.chunk_idx, e.vector, e.text, e.created_at
         FROM embeddings e
         JOIN embedding_models m ON m.model_id = e.model_id
         JOIN objects o          ON o.id = e.object_id
        WHERE m.provider <> 'legacy-blob';
   DROP TABLE embeddings;
   ALTER TABLE embeddings_v2 RENAME TO embeddings;
   CREATE INDEX idx_embeddings_model ON embeddings (model_id, object_id);
   ```

   No table references `embeddings`, so dropping it while foreign keys are on is safe. `id INTEGER PRIMARY KEY` is required because SQLite documents that `VACUUM` may renumber the implicit rowids of tables without one, which would desynchronize the vec0 tables.
2. Remove the placeholder:

   ```sql
   DELETE FROM index_signatures
    WHERE signature_id IN (SELECT 'embeddings_' || model_id FROM embedding_models WHERE provider = 'legacy-blob');
   DELETE FROM embedding_models WHERE provider = 'legacy-blob';
   ```
3. For each remaining registered model, call `EnsureIndex(SpecFor(model))`. Skip, with a `slog.Warn`, any model whose ID fails `ValidateEmbeddingModelID` or whose dimension is outside `1..8192`. Never fail the migration for them.

After the migration loop, `Migrate()` also runs the same `EnsureIndex` pass every time, unversioned, for every registered model. That pass is the ADR-070 verify-and-rebuild on each open.

The per-model objects that `EnsureIndex` creates (with `<h>` = `EmbeddingIndexName(model_id)` minus its `emb_` prefix, `<id>` = the validated model_id literal and `<dim>` = the registry dimension):

```sql
CREATE VIRTUAL TABLE vec_emb_<h> USING vec0(embedding float[<dim>] distance_metric=cosine);
CREATE TRIGGER trg_vec_emb_<h>_ins AFTER INSERT ON embeddings WHEN new.model_id = '<id>'
  BEGIN INSERT INTO vec_emb_<h>(rowid, embedding) VALUES (new.id, new.vector); END;
CREATE TRIGGER trg_vec_emb_<h>_upd AFTER UPDATE OF vector ON embeddings WHEN new.model_id = '<id>'
  BEGIN DELETE FROM vec_emb_<h> WHERE rowid = old.id;
        INSERT INTO vec_emb_<h>(rowid, embedding) VALUES (new.id, new.vector); END;
CREATE TRIGGER trg_vec_emb_<h>_del AFTER DELETE ON embeddings WHEN old.model_id = '<id>'
  BEGIN DELETE FROM vec_emb_<h> WHERE rowid = old.id; END;
-- refill on create/rebuild:
INSERT INTO vec_emb_<h>(rowid, embedding) SELECT id, vector FROM embeddings WHERE model_id = '<id>';
```

These behaviours were probed against sqlite-vec v0.1.6: triggers keep vec0 in sync on insert, upsert and delete, and on `ON DELETE CASCADE` from `objects`; a wrong-dimension row aborts the statement; vec0 DDL is transactional. vec0 rejects `INSERT OR REPLACE` and `ON CONFLICT` upserts, so the update trigger deletes and then inserts. The signature is `ComputeEmbedding(model_id, provider, dim, "vec0", "cosine", <vec_emb_<h> DDL>)`. `Search` is:

```sql
SELECT e.object_id, e.chunk_idx, v.distance
  FROM vec_emb_<h> v JOIN embeddings e ON e.id = v.rowid
 WHERE v.embedding MATCH ? AND k = ?
 ORDER BY v.distance
```

`PurgeModel` drops the three triggers, then drops the vec0 table, then deletes the rows and the signature.

### SQLite 038 — `drop single-vector path` (legacy sweep)

```sql
DROP TABLE IF EXISTS vec_objects;
DROP TABLE IF EXISTS object_embeddings;
ALTER TABLE objects DROP COLUMN embeddings;   -- only when pragma_table_info('objects') has it
```

`DROP COLUMN` was probed on SQLite 3.53.4 against a plain BLOB column. Before dropping, the sweep must confirm that no index, trigger or view in `sqlite_master` references `objects.embeddings`.

### Postgres 14 — `per-model embeddings` (per-model index task)

This is a Go function. It runs inside the existing advisory lock.

1. Delete orphaned and placeholder rows:

   ```sql
   DELETE FROM embeddings e
    WHERE NOT EXISTS (SELECT 1 FROM objects o WHERE o.id = e.object_id)
       OR e.model_id IN (SELECT model_id FROM embedding_models WHERE provider = 'legacy-blob');
   ```
2. Add `embeddings_object_fk FOREIGN KEY (object_id) REFERENCES objects(id) ON DELETE CASCADE` and `embeddings_chunk_idx_nonneg CHECK (chunk_idx >= 0)`. Guard each with a `pg_constraint` lookup.
3. Remove the placeholder's signature and model rows, as in SQLite step 2. `$`-rebind the queries.
4. For each registered model, call `EnsureIndex`. Skip, with a warning, any model whose ID is invalid or whose dimension is outside `1..2000`.

After the migration loop, run the same `EnsureIndex` pass every time.

The per-model index (`idx_emb_<h>`) and query:

```sql
CREATE INDEX IF NOT EXISTS idx_emb_<h> ON embeddings
  USING hnsw ((vector::vector(<dim>)) vector_cosine_ops) WHERE model_id = '<id>';

SELECT object_id, chunk_idx, vector::vector(<dim>) <=> $1::vector(<dim>) AS distance
  FROM embeddings
 WHERE model_id = '<id>'
 ORDER BY vector::vector(<dim>) <=> $1::vector(<dim>)
 LIMIT $2;
```

The `model_id` predicate **must be a literal**. It is safe because of the charset rule. The probe on pgvector 0.8.6 showed a generic plan with a bound `$n` falling back to a bitmap scan plus sort. Honor `caps.iterativeScan` as the other vector paths do. The signature is `ComputeEmbedding(model_id, provider, dim, "hnsw", "vector_cosine_ops", <WITH (...) params>)`. `PurgeModel` runs `DROP INDEX` and then deletes the rows and the signature.

### Postgres 15 — `drop objects.embedding` (legacy sweep)

`ALTER TABLE objects DROP COLUMN IF EXISTS embedding`. This also drops `idx_objects_embedding_hnsw` (probed).

## Go interface changes by task

### Register probe

- `runEmbeddingsRegister` does the following in order:
  1. Validate the model ID with `storage.ValidateEmbeddingModelID`.
  2. Call `ProviderResolver.ForRegistration` to get the provider and the `config_json` to store.
  3. Embed a fixed probe string and take `dimension = len(vector)`.
  4. If `--dimension` was given, it becomes a check: a mismatch is an error.
  5. Store `provider` and `config_json` in the registry row.
  6. Call `registry.Register`.
  7. Call `driver.Embeddings().EnsureIndex(embeddings.SpecFor(model))`.
- `$EDITOR` is required only when neither flags nor configuration determine the config.
- In `registry.Register`: validate the model ID, and set `maxIndexableDim = storage.SQLiteVecMaxDimension` for SQLite. The Postgres ceiling already exists.

### Per-model index

- Replace both stub `EmbeddingStore`s.
- Add `indexsig.SQLiteVectorIndexFor(ctx, db, table)` and `indexsig.PostgresVectorIndexFor(ctx, db, indexName)`. Keep the existing `SQLiteVectorIndex` and `PostgresVectorIndex`, because historic migrations 033–035 and 12 call them.
- Add the migrations and the post-migrate `EnsureIndex` pass.
- Add a shared conformance suite, `storagetest.EmbeddingStoreConformance(t, drv)`, run by both drivers. It covers:
  - per-model dimensions (for example 4 and 8 in one database)
  - wrong-dimension `Put` rejected with no partial write
  - `Put` replace semantics
  - cascade on object delete
  - signature mismatch triggering a rebuild
  - `PurgeModel`
  - `ListMissing` paging
  - `Search` ordering and chunk collapse

### Ingest writes

- `steps.NewEmbeddingGenerator(models embeddings.ModelSource, resolver embeddings.ProviderResolver) *EmbeddingGenerator`.
  - The contract becomes `Produces: ["Vectors", "VectorIndexed"]`.
  - `Run` does the following:
    1. Take `text = projection.ProjectIndex(draft).EmbeddingText`. If it is empty, stop.
    2. For each model `m` in `Populating(now)`: resolve `ForModel(m)`, call `Embed(text)`, and require `len == m.Dimension`. On success, append `ObjectVector{ModelID: m.ModelID, ChunkIdx: 0, Vector, Text: text}`. On failure, log a `slog.Warn` with the model_id and continue.
    3. Set `draft.VectorIndexed` to true only if the default model succeeded.
  - Nil dependencies mean a no-op. With no default model, it logs one warning per process.
- `steps.NewDedupStep(models embeddings.ModelSource, store storage.EmbeddingStore, cfg config.DuplicatesConfig)`. It takes the default model's vector from `draft.Vectors`, searches with `store.Search`, and uses `Requires: ["Vectors"]`. It must not call `ObjectStore.VectorSearch`.
- `builtins.BuildOpts` gains `Models embeddings.ModelSource`, `Resolver embeddings.ProviderResolver` and `Embeddings storage.EmbeddingStore`, and `embedding` and `dedup` get dependency-aware constructors. A per-pipeline `providers.embedding` override is ignored with a warning: the model's registry entry decides the provider.
- Persistence: in `internal/jobs/worker.go`, after `Create` or `Reinforce` (on the reinforce path, use the existing ID); and in `internal/service/reanalyze.go`, after `Update`. When `len(draft.Vectors) > 0`, call `store.Embeddings().Put(ctx, id, draft.Vectors)`. On error, log a warning; the job does not fail.
- Wiring: `registry.ForDriver(driver)`, the resolver constructor and `driver.Embeddings()` go into `BuildOpts` in both `helpers.go` files and in `serve.go`.

### Query path

- `ObjectStore.VectorSearch(ctx, q storage.VectorQuery, filter ObjectFilter) ([]*KnowledgeObject, error)` and `VectorSearchNodeAware(ctx, q storage.VectorQuery, filter, naf)`:
  - The ranking comes from the model's index and the result sets `Metadata["score"] = 1 - distance`.
  - On SQLite: KNN over-fetch (×10, as today) through `EmbeddingStore.Search`, then hydrate and filter. `ObjectStore` gets an `emb storage.EmbeddingStore` field that `driver.go` sets from `d.Embeddings()`. Never construct `EmbeddingStore{...}` literals outside `embeddings.go`, because the per-model index task owns its fields.
  - On Postgres: a join with the object filters in SQL, using the per-model literal predicate and cast.
- Remove `ListWithEmbeddings`, `ListWithoutEmbeddings`, the `VectorStore` interface, `StorageDriver.Vectors()`, `VectorHit`, both `vectors.go` files, and the ANN fields and `object_embeddings` / `objects.embedding` writes in both `objects.go` files.
- Service: the search methods stop taking a `providers.EmbeddingProvider` and take a semantic source instead: `embeddings.ModelSource`, `embeddings.ProviderResolver`, and an optional per-model session-blend hook. Per query, read `Default`, resolve `ForModel`, embed, check the dimension, and search.
  - `SearchDiagnostics` gains a semantic-leg status: `ok`, `no_default_model`, `provider_error`, `dimension_mismatch`, `index_missing` or `low_coverage`.
  - `find` prints a notice whenever the status is not `ok`. Never degrade silently.
- Session query vectors are stored keyed by `model_id`. A mismatched model's vectors are dropped, never blended.
- `service.ReindexVectors` and `dpkms dev reindex-vectors` are removed; the migrate command replaces them.
- `lint` duplicate checking is ported to `EmbeddingStore.Get` and `Search` on the default model, and its threshold compares similarity (`1 - distance`), fixing the distance-versus-similarity bug.
- `service/duplicates.go` and `internal/retrieval` move to the new `VectorSearch`.

## File ownership (parallel split)

No file appears under two tasks. **New test files are named per task** so that no two tasks edit the same `_test.go`.

| Task | Owns (edit or create) |
|---|---|
| **Register probe** | `cmd/ctxt/cmd/embeddings.go`; `internal/embeddings/registry/registry.go`; new `internal/embeddings/registry/register_probe_test.go`; new `cmd/ctxt/cmd/embeddings_register_test.go` |
| **Per-model index** | `internal/storage/sqlite/{embeddings.go,migrations.go}`; new `internal/storage/sqlite/{embeddings_test.go,migration037_test.go,conformance_embeddings_test.go}`; `internal/storage/sqlite/migration034_test.go` (placeholder assumptions); `internal/storage/postgres/{embeddings.go,migrations.go,migration_ledger_test.go}`; new `internal/storage/postgres/{embeddings_test.go,conformance_embeddings_test.go}`; `internal/storage/indexsig/{indexsig.go,indexsig_test.go}`; new `internal/storage/storagetest/embeddings_conformance.go`; `internal/embeddings/registry/registry_test.go` (placeholder-seed assumptions only); `internal/embeddings/registry/registry_pg_test.go` |
| **Ingest writes** | `internal/pipeline/steps/{embedding.go,embedding_test.go,dedup_step.go,dedup_step_test.go}`; `internal/pipeline/builtins/builtins.go` + its tests; `internal/jobs/worker.go` + its tests; `internal/service/reanalyze.go` + its tests; `cmd/ctxt/cmd/helpers.go`; `cmd/dpkms/cmd/{serve.go,helpers.go}` |
| **Query path** | `internal/storage/storage.go`; `internal/storage/storage_test.go`; `internal/storage/types.go` (`VectorHit`); `internal/storage/sqlite/{objects.go,vectors.go,driver.go}` + `{vectors_test.go,conformance_vector_test.go,objects_test.go,projection_wiring_test.go}`; `internal/storage/postgres/{objects.go,vectors.go,driver.go}` + `{vector_store_test.go,vector_search_test.go,vectors_unit_test.go,conformance_vector_test.go,reinforce_flags_test.go}`; `internal/storage/storagetest/{vector_rank.go,search_conformance.go}`; `internal/service/{service.go,duplicates.go}` + `{service_test.go,service_search_test.go,duplicates_test.go}`; `cmd/ctxt/cmd/{find.go,find_test.go,dev_test.go}` + session-blend code; `cmd/dpkms/cmd/dev.go`; `internal/retrieval/*`; `internal/lint/checks.go`; ObjectStore/StorageDriver fakes in `internal/steps/executor_test.go`, `internal/ingest/runner_test.go`, `internal/ingest/cardamum/teststore_test.go`, `internal/pipeline/steps/alternative_detector_test.go`; `test/integration/{us0051_semantic_search_test.go,us0061_visual_similarity_test.go,simulation_test.go}` |
| **Legacy sweep** (sequential, after ingest and query merge) | SQLite 038 and Postgres 15 (appending to both `migrations.go`); remove `KnowledgeObject.Embeddings` and its readers (`plugins/ica/*` + tests, `internal/proximity/proximity.go`); remove `Driver.vectorDimension` / `SetVectorDimension` / `DefaultVectorDimension` (inline 1536 into historic migrations 020/033/034 and Postgres 6/11; callers in `internal/storage/sqlite/driver_test.go`, `internal/storage/postgres/search_schema_test.go`, `internal/embeddings/registry/registry_test.go`); remove `registry.ProviderLegacy`, `LegacyEmbeddingModelID` when unused |

Shared files the four tasks must **not** edit: every contract file listed above, `internal/config/config.go`, and `internal/providers/*` (the resolver's files).

Two constraints on the resolver, so that it does not collide with this split:

- Its runtime-override flags must be registered from its own files (for example a helper that attaches flags to commands by name in an `init()`), not by editing `find.go` or `embeddings.go`.
- It must satisfy `embeddings.ProviderResolver` structurally. The compile-time assertion lives outside `internal/providers`, because of the import cycle.

### Merge order

1. Per-model index first, because it replaces the stubs, and the others' end-to-end tests need real storage.
2. Register probe, in any order after that.
3. Ingest writes **before** query path, because the query path deletes the old `ObjectStore.VectorSearch` signature that `dedup_step.go` calls on `main` until ingest writes lands.
4. Legacy sweep last.

Each task rebases onto `main` before merging. Disjoint ownership keeps those rebases free of conflicts.

## Test gates per task

- **All tasks:** `go test -tags fts5 -race ./...` is green, and `/usr/bin/env go build -buildvcs=false ./...` succeeds. Postgres paths run under `-tags=integration,fts5` with `POSTGRES_*`. Never contact the live servers on :8080 or :8081; use temp directories only.
- **Per-model index:** the conformance suite runs on both drivers, plus migration tests for fresh and upgraded databases:
  - An upgraded database seeded with a placeholder row and a non-placeholder row ends with only the non-placeholder row and an index.
  - Reopening is idempotent.
  - Mutation checks: remove the dimension guard in `Put`, the trigger, or the literal predicate. The tests must fail.
- **Ingest writes:** a pipeline run with a fake resolver and two populating models writes two rows. A failing model writes one row and the job still succeeds. The dedup step uses the default model only.
- **Query path:** with no default, `find` reports `no_default_model` and returns FTS results. With a default and coverage, it returns semantic hits whose score is `1 - distance`. After a default flip in another handle, the next query uses the new model with no restart.
- **Register probe:** use an xrr cassette (or an `httptest` Ollama stand-in) that returns 1024-dimensional vectors, then check that a `--dimension 768` mismatch errors, that the index exists after registration, and that an invalid model ID is rejected.

## Open questions (none block the four tasks)

- **`objects.vector_indexed`:** kept as an advisory flag ("the default model produced a vector at ingest"). It goes stale after a default flip. Drop it in the sweep, or recompute it on flip?
- **`embeddings.text`:** the ADR keeps chunk text for eval. With single-chunk embedding it duplicates the projected body once per model. Keep it, or store it only for the default model?
- **Postgres dimension backstop:** `Put` enforces the registry dimension in Go. A per-model `CHECK (model_id <> '<id>' OR vector_dims(vector) = <dim>)` would add a database-level guard. This is unprobed, so adopt it only if the per-model index task verifies it.
- **Tracking the legacy sweep:** it needs its own task, blocked by ingest writes and query path.
