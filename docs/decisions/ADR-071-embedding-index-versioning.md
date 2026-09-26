# ADR-071 – Embedding Index Versioning and Dual-Write Migration

> **Status:** Accepted, amended 2026-09-26 (see [Amendment 2026-09-26](#amendment-2026-09-26))
> **Date:** 2026-05-07
> **Author:** jadb
> **Applies to:** dpkms, vector storage layer, embedding pipeline step, ctxt CLI
> **Supersedes:** None
> **References:** ADR-070 (upgrade taxonomy + pipeline_version + index signatures — precondition for this ADR), ADR-004 (step-based pipeline), ADR-013 (knowledge graphs), ADR-061 (search quality tiers), T-0578 (this ADR's source task)

---

## Context

ctxt today has a single embedding index. Swapping the embedding model — for any reason: cheaper, better, sovereignty-required, provider-deprecated — is a `reingest_all` event in the ADR-070 taxonomy: the worst-shaped upgrade in the system. Brutal cutover, full corpus re-embedded before any query benefits, no fallback during transit, no way to verify the new model is actually better before committing.

This shape is unacceptable for what should be a routine concern. Embedding models will change — the question is how to handle it gracefully, not whether.

The same machinery that solves the migration problem — **multiple concurrent embedding indexes** — also unlocks bonus capabilities the team can defer building: heterogeneous model routing (code vs. multilingual vs. long-context), sovereignty/cost tiers, on-device fallback. Those are nice-to-haves, not the driver. Building only the migration capability today, while keeping the data model open for the rest, is the right tradeoff.

### Driver use case (must solve)

**Graceful model migration without hard cutover or query blackout.** The operator should be able to:

1. Register a new candidate embedding model alongside the current production one.
2. Begin dual-writing: every new ingest produces embeddings for *both* models.
3. Background-migrate existing objects to the new model at a controlled rate (rate-limited, observable, interruptible).
4. Verify recall improvement on a fixed query corpus before flipping the default (using `hop.top/ben`).
5. Atomically flip the default once coverage + verification thresholds are met.
6. Retain the old index as fallback for a configurable grace period.
7. Decommission the old model on a scheduled date once confidence is high.

### Bonus use cases (must not foreclose, must not build)

These shape the data model but do not justify implementation today:

- **Query-time multi-index routing** — route queries by content tag, language, recency, etc.
- **Score fusion across multiple vector indexes** — RRF, weighted sum, learned reranker.
- **Per-tenant / per-profile model selection.**
- **On-device-only embedder for sovereignty subset.**
- **Cost-aware tiering** — cheap embedder for archive, premium for hot queries.

The data model accommodates all of these. The control plane and query path do not implement any of them in this phase.

### Three preconditions before any implementation

The ADR itself is approved. Implementation tasks spawned from this ADR (T-0582 through T-0585) are gated on:

1. **ADR-070 must be landed and shipping** — pipeline-version and index-signature machinery must work end-to-end. As of 2026-05-07 ADR-070's Phases 1–3 (T-0579, T-0580, T-0581) are shipped; precondition met.
2. **An eval harness must exist** that can answer "is the new model helping?" on a fixed query corpus. `hop.top/ben` is the chosen tool (per ADR-070); a recall suite at `suites/recall-vector.ben.yaml` must exist before any `ctxt embeddings migrate` command ships.
3. **A second concrete use case** must earn its keep beyond migration — i.e. the team has hit a real heterogeneous-corpus or sovereignty pain point. Migration alone justifies the **transitional dual-write capability**; the **full multi-engine routing layer** waits for demonstrated need.

If precondition 3 hasn't fired by the time precondition 1+2 are met, build only the dual-write transitional capability and stop.

---

## Decision

We adopt **a single `embeddings` table with `(object_id, model_id, chunk_idx)` composite key**, plus a **model registry**, plus **dual-write at ingest time during migration windows**, plus **explicit operator-driven default-flip** once recall verification passes.

The query path remains single-default for now: queries always hit the index of the currently-default `model_id`. Multi-index routing is designed-for but not built.

### Data model

```sql
-- Model registry: one row per registered embedding model
CREATE TABLE embedding_models (
    model_id            TEXT PRIMARY KEY,        -- e.g. "openai-text-embedding-3-small@2025-01-15"
    provider            TEXT NOT NULL,            -- "openai", "ollama", "voyage", etc.
    dimension           INTEGER NOT NULL,
    is_default          INTEGER NOT NULL DEFAULT 0,
    registered_at       TEXT NOT NULL,
    deprecated_at       TEXT,                    -- nullable; set when model_id is being phased out
    config_json         TEXT NOT NULL DEFAULT '{}'
);

-- Single embeddings table; rows keyed by (object, model, chunk)
CREATE TABLE embeddings (
    object_id           TEXT NOT NULL,
    model_id            TEXT NOT NULL REFERENCES embedding_models(model_id),
    chunk_idx           INTEGER NOT NULL,
    vector              BLOB NOT NULL,           -- serialized per-provider format
    text                TEXT,                    -- the chunk text that was embedded (for debugging/eval)
    created_at          TEXT NOT NULL,
    PRIMARY KEY (object_id, model_id, chunk_idx)
);

CREATE INDEX idx_embeddings_model ON embeddings (model_id, object_id);
```

Notes:

- **Single table, composite key.** Per-model tables (`embeddings_v1`, `embeddings_v2`) were the alternative; rejected because every join, every coverage query, every administrative SQL grows quadratically in number of registered models. The composite key approach is one query plan.
- **`text` column** retained per chunk so the eval harness (`hop.top/ben` recall suite) can verify the new model is indexing the right text without round-tripping through the projection layer.
- **`is_default` flag** on `embedding_models` selects which model the query path uses. At most one row has `is_default = 1` (enforced by partial unique index, see below).
- The signature stamping from ADR-070 (`index_signatures` table) gains an `embeddings_<model_id>` row per registered model, hashed over (model_id + dimension + provider). Same auto-rebuild semantics as `objects_fts`.

```sql
CREATE UNIQUE INDEX idx_embedding_default ON embedding_models(is_default) WHERE is_default = 1;
```

### Pipeline behavior at ingest

The `embedding` step in the pipeline reads from a **policy** that names the active models to populate:

```yaml
# In dpkms.yaml or contexthelp policy.d/
embeddings:
  populate_models:
    - openai-text-embedding-3-small@2025-01-15  # current default
    # During a migration, add the candidate:
    - openai-text-embedding-3-large@2026-05-01  # candidate
```

The step iterates `populate_models`, generates embeddings under each, writes one row per (object × model × chunk). If a model is unreachable, the step records the failure but does not block the ingest — partial coverage is acceptable during migration windows; the migration job backfills.

Outside a migration window, `populate_models` contains exactly one entry: the current default. Steady-state cost is one embedding call per ingest, identical to today.

### Migration job

`ctxt embeddings migrate --to <model_id> [--rate-limit N/s] [--budget-usd N]` runs a background re-embed:

1. Queries `embeddings` for `object_id` rows missing a `(object_id, <new_model_id>)` tuple.
2. Re-runs the embedding step against `<new_model_id>` only (not the full pipeline).
3. Writes new rows to `embeddings`.
4. Surfaces progress via the ADR-070 upgrade-status surface (`ctxt upgrade status` includes embedding-migration state).
5. Honors `--rate-limit` (calls/second) and `--budget-usd` (cumulative cost cap; halts and asks for re-confirmation if exceeded).

The migration is restartable, idempotent, and can be interrupted with `ctxt upgrade run --pause` (per ADR-070 upgrade CLI).

> **As built (2026-09-26):** `migrate` takes `--to`, `--rate-limit`, `--batch` and `--dry-run`. There is no `--budget-usd` cost cap and no pause: a run is stopped with `dpkms job cancel <job-id>` and resumed by running `migrate` again. Progress is reported in `ctxt upgrade status` under bucket `embeddings_migrate`, with `target` and `failed`. See [Operate embedding models](../manual/operations/embeddings.md#migrate).

### Default-flip control

```
ctxt embeddings list                           # show registered models, coverage %, default
ctxt embeddings register <model_id>            # add a candidate
ctxt embeddings deprecate <model_id> --on <date>  # schedule retirement
ctxt embeddings set-default <model_id>         # atomic flip
```

`set-default` is gated:

- Refuses unless the candidate has ≥99% coverage (configurable threshold).
- Optionally runs a `hop.top/ben` recall suite as a pre-flight check; if recall regresses below tolerance, refuses unless `--i-know-recall-regressed` is passed.
- Atomically updates `is_default` and emits a bus event that invalidates any in-flight query plans.

> **As built (2026-09-26):** only the coverage guard exists (`embeddings.min_coverage`, default 0.99; `--min-coverage` per call). There is no recall pre-flight and no `--i-know-recall-regressed` flag.

The old index is **not deleted** on flip. It remains queryable as fallback for the configured grace period (default 30 days, configurable). Decommissioning happens via a separate `ctxt embeddings deprecate` command that schedules removal.

### Query path (this phase)

The query path reads `model_id` from the registry's `is_default = 1` row at startup, caches it, and uses that exclusively. No per-query routing logic. No score fusion. The `embeddings` table's other rows exist as data the migration job populates and the next default-flip will activate — but they're not queried.

This is the explicit "design-for, don't-build" line: the table is multi-model, the query path is single-default. The day a real heterogeneous-corpus or sovereignty use case lands, the query path can grow routing logic without a data-layer migration.

### Operator-facing CLI surface (added to ADR-070's `ctxt upgrade *` subtree)

- `ctxt embeddings list` — registered models, coverage %, default flag, deprecation status
- `ctxt embeddings register <model_id>` — add a candidate; the provider comes from the `--embedding-*` flags, `CTXT_EMBEDDING_*` env, `-c providers.embedding.*` or config, and its dimension is measured by embedding a probe string (`--dimension` only checks it)
- `ctxt embeddings migrate --to <model_id>` — kick off a background migration job
- `ctxt embeddings set-default <model_id>` — atomic default-flip with the coverage guard
- `ctxt embeddings deprecate <model_id> --on <date>` — schedule retirement
- `ctxt embeddings purge <model_id>` — actually delete embedding rows for a deprecated model (post-grace-period)

The migration job surfaces progress via the ADR-070 `ctxt upgrade status` infrastructure (bucket `embeddings_migrate`); the other commands complete synchronously. Migration jobs and default flips are bus-event-emitting per ADR-070 conventions.

### Test strategy

- **`hop.top/ben`** — `suites/recall-vector.ben.yaml` is **mandatory before any `embeddings migrate` ships**. The suite runs a fixed query corpus against (a) current default, (b) candidate model, and reports per-query recall delta. PR-time CI runs the suite on the cassette-recorded fixture set.
- **`hop.top/xrr`** — cassettes for embedding-provider HTTP calls during the migration job. New cassettes live at `test/integration/testdata/cassettes/embeddings_migrate_<model_id>/`. The migration job's external-call surface is small (one POST per chunk) so the cassette set is tractable.
- **`hop.top/eva`** — contract for `ctxt embeddings list` JSON output and `embedding-models.is_default` invariant: `contracts/embeddings-list.eva.yaml`. Tier-1 deterministic (JSON Schema). Catches shape regressions before they reach operator CLI consumers.

The migration-job test path:

1. Recorded xrr cassettes for embedding API calls.
2. ben suite runs against (recorded) corpus + fixed query list.
3. Migration job runs end-to-end against the cassettes; ben verifies recall on the resulting embeddings.
4. eva contract validates the `ctxt embeddings list` output shape pre/post.

This is testable in CI without external API calls and without LLM credits (cassette-replayed).

---

## Rationale

### Why composite-key single table, not per-model tables?

Considered both. Composite-key won on:

- **Coverage queries are single-table joins** — "how many objects have embeddings under model X but not model Y" is a single `LEFT JOIN`, not a UNION across N tables.
- **Migration-job is a single SQL pattern** — `WHERE NOT EXISTS (SELECT 1 FROM embeddings WHERE model_id = ?)`, doesn't change per-model.
- **Schema migrations are O(1)** — adding a new model means inserting one row into `embedding_models`, no DDL.
- **Storage cost is identical** — same total bytes regardless of partitioning strategy.

Per-model tables won only on isolation (corrupted index in one table doesn't affect others) — but at our scale, single-table corruption is recovered the same way either approach: from snapshot. Not a real win.

### Why dual-write only during migration, not always?

Steady-state dual-write costs 2× embedding API calls per ingest, every ingest. Over years, that's significant. We accept the cost during a migration window (typically days-to-weeks); we reject it as a permanent default. The operator flips into dual-write mode by adding a model to `populate_models`, and out of it by removing the old model after the grace period.

### Why operator-driven default-flip, not automatic?

A `set-default` flip is a `reingest_all`-equivalent in user impact — every query result changes. Automatic flips remove operator agency over what is functionally a major version bump. The coverage + recall guards are the safety net, but the final consent is human.

The `--i-know-recall-regressed` escape hatch exists because some legitimate flips trade off recall for other properties (cost, sovereignty, latency). The operator must explicitly acknowledge.

### Why bake the model into `model_id` as `<provider-name>@<date>`?

Three reasons:

1. **Providers update models without notifying.** OpenAI's "text-embedding-3-small" today is not necessarily the same as 18 months from now. The date suffix locks the model identity to a wall-clock decision.
2. **Human-readable in `ctxt embeddings list`** — operators see exactly what they're running.
3. **No ambiguity in cassettes** — the xrr cassette filename includes `model_id`, so cassettes for "today's openai-3-small" and "next year's openai-3-small" can coexist.

### Why the ben + xrr + eva trio specifically?

Same rationale as ADR-070, applied to embeddings:

- **xrr** — embedding API calls are external and rate-limited; cassettes are essential to keep CI deterministic and free.
- **ben** — recall-on-a-fixed-corpus is the only way to verify a model swap is helping vs. hurting. Without ben, every model migration is a vibes-based decision.
- **eva** — the `ctxt embeddings list` output is consumed by humans and (eventually) by dashboards. eva contracts catch shape regressions before they reach the consumer.

The eval harness is the single biggest piece of infrastructure this ADR depends on. ben is the chosen tool because it's already designed around the "which candidate is better, by how much" question, and because adopting it alongside ADR-070 means we're paying the learning-curve cost once, not twice.

### Alternatives considered

- **"Hot-swap the embedding model in place; re-embed every existing object on next read"**: rejected — collapses bucket 2/3 into bucket 3 with no operator control over rate or cost.
- **"Maintain only the current model's embeddings; backfill on demand at query time"**: rejected — query-time embedding generation is a latency disaster (multi-second tail) and re-introduces the LLM-credit cost into the query path.
- **"Defer this entire ADR until the first real model swap"**: rejected — the same argument that ADR-070 rejects. The cost of the framework is one ADR; the cost of *not* having it is a forced bucket-3 event the next time a provider deprecates a model.
- **"Build full multi-engine routing now (per-tag/per-language)"**: rejected per precondition #3 — design-for-it, don't-build-it, until a real use case lands.

---

## Consequences

### Positive

- **Graceful model migration** without query blackout, with verifiable recall improvement before flip.
- **Provider deprecation is no longer an emergency** — the new model can be brought up and verified before the old one is shut off.
- **Multi-engine routing is unblocked** for the day a real use case justifies it; data model already supports it.
- **Cost transparency** — `--budget-usd` cap prevents runaway migrations.
- **Testable end-to-end** without LLM credits.

### Negative

- **Storage during migration** — 2× embeddings for every object that's been migrated. At current corpus size this is negligible (KB scale); at 100× scale it's medium (sub-100 MB). We accept.
- **Schema complexity** — three tables (`embedding_models`, `embeddings`, `index_signatures`) where one existed. The query path remains simple (single default lookup), but the operator surface and migration job add real cognitive load.
- **`hop.top/ben` adoption** — second new dependency in a week (eva is the first, per ADR-070). Justified by precondition #2 (no migration without an eval harness).

### Neutral or considerations

- The deprecation policy (default 30 days grace) is a user-facing value the operator will tweak. Choosing 30 was a "round-month" call; we'll revisit if real usage shows it's wrong.
- Signed-off-by chain on `set-default --i-know-recall-regressed` is not currently audited. If multi-operator deployments emerge, an audit-log entry per flip becomes necessary.
- Federation (ADR-064) peers will eventually need to negotiate which `model_id` they'll cross-query under. Out of scope for this ADR; future federation ADR if it becomes a problem.

---

## Implementation Notes

### Phase 1 — Data model + registry (T-0582)

- Migration: `embedding_models`, `embeddings` table replacing the current single index.
- Registry CRUD APIs (Go side) + minimal CLI (`ctxt embeddings list`, `register`).
- Tests: in-package unit tests; eva contract for `list` output.

### Phase 2 — Dual-write at ingest (T-0583)

- Pipeline `embedding` step reads `populate_models` policy.
- Policy lives in `dpkms.yaml`. Default is `[<current-default>]`.
- Tests: xrr cassettes for both models' API calls; pipeline step writes both rows.

### Phase 3 — Migration job + recall guard (T-0584)

- Background re-embed worker.
- `ctxt embeddings migrate --to <model>` CLI command.
- ben recall suite (`suites/recall-vector.ben.yaml`) — **mandatory before this phase ships**.
- `ctxt embeddings set-default` with coverage + recall guards.
- Tests: full xrr-recorded migration end-to-end; ben suite executes against the cassette-fixed corpus.

### Phase 4 — Deprecation + purge (T-0585)

- Scheduled deprecation; grace period; `ctxt embeddings purge`.
- Tests: time-travel test via cassettes; verify queries still work during grace, fail after purge.

### Backward compatibility

- Existing single-table embeddings rows are migrated into `embeddings` with a synthetic `model_id` derived from the current pipeline config. `embedding_models` gets the corresponding row marked `is_default = 1`.
- After migration, the schema state is identical to a fresh install.
- No CLI command name conflicts; `embeddings` is a new noun under `ctxt`.

#### Implementation Notes (Phase 1 lock-in)

The synthetic `model_id` derived for legacy-blob backfill follows the format `legacy-blob-<dim>@2026-05-07`, where `<dim>` is the driver's vector dimension at migration time (defaults to 1536 when no dimension is recorded). The `@2026-05-07` suffix is the ADR's authorship date and is **immutable**: subsequent migrations must not change it. Phase 2 (T-0583, dual-write at ingest) ships with `populate_models` defaulting to exactly this string after a fresh upgrade, so changing the anchor would silently break every existing deployment's policy. This anchor was first encoded in T-0582 (commit `4bbf5bf`); treat it as a load-bearing constant.

Postgres has no per-object backfill in Phase 1: postgres carries embeddings on the `objects.embedding` pgvector column and has no legacy `object_embeddings` table to copy from. The Phase 1 migration on postgres only seeds the singleton default row in `embedding_models` plus the corresponding `index_signatures` row. The new `embeddings` composite-key table is empty on postgres until Phase 2 (T-0583) starts dual-writing at ingest. T-0584's migration job must not be surprised by this on postgres backends; on sqlite, the per-row backfill is real and the new table is non-empty from the moment migration 033 runs.

### Out of scope (deferred to future ADRs if/when need arises)

- Per-query model routing logic (tag-based, language-based, recency-based).
- Score fusion across multiple vector indexes.
- Per-tenant model selection (multi-tenant infra doesn't exist yet).
- On-device embedder for sovereignty subset.
- Cost-aware tiering at query time.

---

## References

- ADR-070 — pipeline & index versioning + upgrade taxonomy (precondition)
- T-0577 — ADR-070's task; this ADR's enabler
- T-0578 — task this ADR closes
- T-0565 (closed), T-0576 (closed) — recent upgrade-pain context
- `hop.top/ben` — recall benchmarks; precondition #2
- `hop.top/xrr` — cassettes for embedding API calls
- `hop.top/eva` — contracts for embedding-CLI output shapes

---

## Amendment 2026-09-26

> **Status:** Accepted
> **Implementation plan:** [`docs/plans/2026-09-26-embedding-model-migration.md`](../plans/2026-09-26-embedding-model-migration.md)

This amendment reconciles the code with the decision above and settles what the original text left open: how vectors are indexed per model, where the populate set comes from, and what happens to the pre-registry single-vector path. The composite-key table, the registry, operator-driven default flips, and the single-default query path all stand.

ctxt has never shipped a public release, so there is **no backward compatibility**: the single-vector path and the `legacy-blob` placeholder are removed outright in forward-only migrations, with no shims or compatibility reads.

The following sections of this ADR are **superseded**: "Pipeline behavior at ingest" (the `populate_models` policy file), the caching sentence in "Query path (this phase)", "Backward compatibility", "Implementation Notes (Phase 1 lock-in)" (the `legacy-blob-<dim>@2026-05-07` anchor is no longer load-bearing), and the Postgres paragraph that follows it.

### What the code did on 2026-09-26

- **Ingest never produced vectors.** The builtin pipeline registry constructed the `embedding` step with a nil provider, which falls back to the stub (`Embed` returns nil). No provider-aware constructor existed for it. The `dedup` step was likewise registered with a nil store. Zero coverage on every instance follows from this as much as from the placeholder model.
- **The legacy path was a separate table.** On SQLite, `KnowledgeObject.Embeddings` was persisted by `ObjectStore.Create/Update` into `object_embeddings` and mirrored into the `vec_objects` vec0 table at a fixed dimension (`DefaultVectorDimension = 1536`; `SetVectorDimension` had no production caller). The `objects.embeddings` BLOB column existed but was always written NULL. On Postgres, vectors lived on `objects.embedding vector(1536)` with an HNSW cosine index.
- **The ADR-071 tables were inert.** `embeddings` held only the placeholder backfill, and `embedding_models` held the placeholder as default. Nothing read `embeddings` except the coverage count.
- **The legacy upsert could not update.** `VecStore.Upsert` used `INSERT OR REPLACE`, which sqlite-vec v0.1.6 rejects on an existing key ("UNIQUE constraint failed on … primary key"; probed). Re-embedding an object through `Update` therefore failed.
- **The lint duplicate check compared the wrong quantity.** It compared a cosine *distance* against a *similarity* threshold.

### 1. Single write path

`storage.EmbeddingStore` (reached through `StorageDriver.Embeddings()`) is the only path that writes or reads vectors. Canonical rows live in `embeddings`, keyed by the logical key `(object_id, model_id, chunk_idx)`. The ANN index is derived data, always rebuildable from those rows.

- The pipeline `embedding` step does not touch storage, because the object does not exist yet and a reinforced draft maps to a different object ID. Instead it attaches `[]ObjectVector` (model, chunk, vector, text) to the draft's non-serialized `Vectors` field. Whoever persists the draft calls `Embeddings().Put(objectID, draft.Vectors)` after `Create`, `Reinforce` or `Update`. A failed `Put` is logged per model and never fails the ingest job.
- `Put` replaces the object's rows for each model present and leaves other models alone. It rejects a vector whose length differs from the model's registry dimension, and it skips zero-magnitude vectors, whose cosine distance is undefined.
- `embeddings.object_id` gains `REFERENCES objects(id) ON DELETE CASCADE` on both backends, so deleting an object removes its vectors and, on SQLite through triggers, its index entries.
- A model that fails at ingest (unreachable provider, wrong dimension) leaves no row. The missing row is the durable record: the migration job's `ListMissing` cursor picks it up. The step also logs the model_id and error. Whether an object is embedded under a model is read from the `embeddings` table only; there is no per-object flag (section 8).

### 2. Vector index per model, at the registry's dimension

**Decision: one ANN index per `model_id`, not one per dimension, on both backends.** Its dimension is the registry's *measured* dimension (`register` probes the provider), never a driver default.

**SQLite (sqlite-vec v0.1.6 through the cgo bindings `sqlite-vec-go-bindings v0.1.6`; SQLite 3.53.4).** Each model gets its own vec0 table `vec_emb_<h>` (`embedding float[<dim>] distance_metric=cosine`), where `<h>` is the first 16 hex digits of `sha256(model_id)` (`storage.EmbeddingIndexName`). The vec0 `rowid` equals `embeddings.id`. Per-model `AFTER INSERT / UPDATE OF vector / DELETE` triggers on `embeddings`, filtered `WHEN model_id = '<model_id>'`, keep each index in sync. Probe results that ground this choice:

| Probe | Result |
|---|---|
| Two vec0 tables of different dimension (768, 1024) in one database | both work independently |
| Insert or query with the wrong dimension | rejected: "Dimension mismatch … Expected 768 dimensions but received 1024" |
| Dimension ceiling | `float[8192]` accepted; `float[8193]` rejected ("maximum 8192") |
| `DROP` + `CREATE VIRTUAL TABLE` + fill inside one transaction | commits; a rollback restores the previous table and rows (vec0 DDL is transactional) |
| `DROP TABLE` on a vec0 table | removes all shadow tables (`_chunks`, `_info`, `_rowids`, `_vector_chunks00`) |
| `INSERT OR REPLACE`, and `INSERT … ON CONFLICT DO UPDATE`, on vec0 | fail ("UNIQUE constraint failed", "UPSERT not implemented for virtual table"); `UPDATE` and `DELETE`+`INSERT` work |
| Triggers on a regular table writing into vec0 | insert, upsert (row id preserved) and delete stay in sync; a wrong-dimension row aborts the whole statement, so the canonical row is not written either |
| `ON DELETE CASCADE` from `objects` into `embeddings` | fires the delete trigger; the vec0 entry is removed |
| `partition key` and `+auxiliary` columns | supported in v0.1.6 (kept as the per-dimension fallback; not chosen) |

Per-model wins over per-dimension (one vec0 table per dimension partitioned by `model_id`) for four reasons. Each model's index signature maps to exactly one table's DDL. A rebuild or purge touches one model: `PurgeModel` is `DROP TABLE`, which reclaims the shadow tables, rather than row deletes inside a shared table. A dimension collision between two models couples nothing. The number of tables is bounded by the number of registered models, which is tens at most.

The vec0 table is keyed by `embeddings.id`, so `embeddings` is rebuilt with an explicit `id INTEGER PRIMARY KEY` and a `UNIQUE (object_id, model_id, chunk_idx)` constraint that carries the logical key. SQLite documents that `VACUUM` may renumber the rowids of tables without an explicit `INTEGER PRIMARY KEY`, and that would silently desynchronize the index.

**Postgres (pgvector 0.8.6, probed against `pgvector/pgvector:pg17`).** `embeddings.vector` stays a typmod-less `vector`. Each model gets a partial expression HNSW index:

```sql
CREATE INDEX idx_emb_<h> ON embeddings
  USING hnsw ((vector::vector(<dim>)) vector_cosine_ops)
  WHERE model_id = '<model_id>';
```

The KNN query must repeat both the cast and the predicate (`WHERE model_id = '<model_id>' ORDER BY vector::vector(<dim>) <=> $1::vector(<dim>)`). Probe results:

- An HNSW index on the bare typmod-less column is rejected ("column does not have dimensions"), so the cast is required.
- `vector(2001)` is rejected ("cannot have more than 2000 dimensions for hnsw index"). Registration on Postgres already enforces the 2000 ceiling.
- The planner uses the partial index when `model_id` is a literal or a custom plan binds it. It does **not** use it under a generic plan (`plan_cache_mode = force_generic_plan`), and falls back to a bitmap scan plus sort.
- A row of the wrong dimension under a model makes every query for that model fail ("expected 3 dimensions, not 4"). That is why `Put` must enforce the registry dimension.

The query therefore inlines `model_id` as a literal. This is safe because of the charset rule in section 8. Dropping `objects.embedding` also drops its dependent HNSW index (probed).

### 3. Index signatures per model

Every index is stamped in `index_signatures` as `embeddings_<model_id>`, hashed by `indexsig.ComputeEmbedding(model_id, provider, dimension, method, ops, params)`:

- **SQLite:** method `vec0`, ops `cosine`, params = the model's vec0 DDL.
- **Postgres:** method `hnsw`, ops `vector_cosine_ops`, params = the index `WITH (...)` build parameters (empty at pgvector defaults).

`EnsureIndex` compares the stored stamp with the one computed from the *desired* DDL, which comes from code and the registry dimension. It creates the index when absent, and on mismatch drops, recreates and refills it from canonical rows in one transaction, then re-stamps it. This is ADR-070 bucket 1 (`reindex_auto`) per model. It runs at the end of every driver `Migrate` for each registered model with a measured dimension, and from `register` after the probe.

### 4. The query path reads the default model

This replaces "reads `model_id` … at startup, caches it". The query path reads the `is_default = 1` row **per query**. That is one indexed single-row lookup, and it makes a `set-default` flip in another process (CLI against daemon) effective on the next query without an invalidation protocol. Per query:

1. Read the default model (`registry.Store.Default`).
2. Resolve its provider (`ProviderResolver.ForModel`, section 7).
3. Embed the query and check `len(vector) == dimension`.
4. Search that model's index through `EmbeddingStore.Search`, or a driver join that applies object filters, using the same index.

If there is no default, the provider fails, the dimensions mismatch, or the index is missing, the semantic leg is skipped and the result carries a **visible notice** naming the reason. It never degrades to FTS silently. Session query-vector blending is keyed by `model_id`, so vectors from a previous default are never blended into a query against a new one. Multi-index routing and score fusion remain out of scope.

### 5. The populate set comes from the registry

This replaces the `populate_models` policy in `dpkms.yaml`. Ingest writes vectors for `registry.Store.Populating(now)`, which is:

- the default model, plus
- every registered model whose deprecation is not yet effective (`deprecated_at` is NULL or later than now),
- excluding models without a measured dimension.

Registering a candidate therefore starts dual-writing, and deprecating it (or its predecessor) stops dual-writing. Keeping the populate set in the database instead of a config file means the CLI, which registers models, and a possibly remote daemon, which ingests, cannot disagree, and no restart is needed. Steady state is still exactly one embedding call per ingest, once the old default is deprecated.

### 6. Chunking: single chunk for now

The step embeds one chunk per object: `chunk_idx = 0`, and `text` is `projection.ProjectIndex(draft).EmbeddingText`. That matches ADR-046, which did not adopt chunking strategies. The data model and the `EmbeddingStore` contract are multi-chunk ready. `Put` replaces all of an object's chunks under a model, and `Search` over-fetches and collapses to the best chunk per object. That collapse happens in Go, not in a SQL `GROUP BY` on the KNN leg, which would defeat the index. A chunker can land later without a schema change.

### 7. Provider resolution contract (consumed, not designed here)

Every consumer (ingest step, query path, `register`, migration job) obtains providers through one interface, `internal/embeddings.ProviderResolver`:

```go
type ProviderResolver interface {
    // m's config_json overlaid with transport-only runtime overrides.
    ForModel(ctx context.Context, m registry.Model) (providers.EmbeddingProvider, error)
    // config + runtime overrides (no registry input), plus the config_json to persist.
    ForRegistration(ctx context.Context) (providers.EmbeddingProvider, json.RawMessage, error)
}
```

The invariant consumers rely on: a registered model's `config_json` fixes its vector-space identity (backend and model). A runtime override may change transport, such as the endpoint (for example a remote Ollama over a tunnel) or credentials, but an override that would change a registered model's backend or model must fail. Otherwise it would write or query foreign vectors under that `model_id`. For the same reason the per-pipeline `providers.embedding` override no longer selects the embedding provider. The resolver lives beside the providers and satisfies the interface structurally, since `providers` cannot import `internal/embeddings` without a cycle.

### 8. Removal of the old path and the placeholder

These are forward-only migrations with no compatibility reads.

- **Placeholder.** Every `legacy-blob` model, its `embeddings` rows and its `embeddings_<model_id>` signature are deleted when the per-model schema lands. Migrations 032–035 on SQLite and 6/10–12 on Postgres keep running on fresh installs and are then superseded. After this, an instance with no registered model has no default, and search reports that it is FTS-only until an operator registers a model, migrates and flips the default.
- **Single-vector path.** Removed in a final sweep, after both the ingest and query paths have moved to `EmbeddingStore`:
  - on SQLite: the `vec_objects` and `object_embeddings` tables, the `objects.embeddings` column, `VecStore`, the fixed-dimension driver plumbing and `KnowledgeObject.Embeddings`;
  - on Postgres: `objects.embedding` and its HNSW index;
  - on both: `objects.vector_indexed` and `KnowledgeObject.VectorIndexed`, which went stale on every default flip.
- **Model-ID charset.** `model_id` is restricted to `^[A-Za-z0-9][A-Za-z0-9._:@/+-]{0,199}$` (`storage.ValidateEmbeddingModelID`), because it is embedded as a literal in per-model DDL (SQLite trigger `WHEN` clauses, Postgres partial-index predicates and queries).
- **Dimension ceilings.** 8192 on SQLite (vec0) and 2000 on Postgres (HNSW). Both are enforced at `register`.

### Consequences of this amendment

- **Positive:** vectors have exactly one durable home and one write path, and models can have different dimensions on both backends. Each model's index rebuilds, purges and verifies on its own. Search degradation is always visible, and the populate set cannot drift between the CLI and the daemon.
- **Negative:** SQLite now keeps two copies of every vector, the canonical BLOB and the vec0 entry, which is the same trade the legacy `object_embeddings` + `vec_objects` pair made. Per-model triggers and DDL are generated at runtime from registry data, so model IDs have a restricted charset. On Postgres the per-model KNN query must be built with a literal predicate and cast.
- **Neutral:** `set-default`, `deprecate` and `purge` keep their semantics from the Decision section. `purge` becomes `EmbeddingStore.PurgeModel` plus removal of the registry row.
