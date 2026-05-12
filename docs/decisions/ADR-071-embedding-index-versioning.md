# ADR-071 – Embedding Index Versioning and Dual-Write Migration

> **Status:** Accepted
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

### Default-flip control

```
ctxt embeddings list                           # show registered models, coverage %, default
ctxt embeddings register <model_id> <config>   # add a candidate
ctxt embeddings deprecate <model_id> --on <date>  # schedule retirement
ctxt embeddings set-default <model_id>         # atomic flip
```

`set-default` is gated:

- Refuses unless the candidate has ≥99% coverage (configurable threshold).
- Optionally runs a `hop.top/ben` recall suite as a pre-flight check; if recall regresses below tolerance, refuses unless `--i-know-recall-regressed` is passed.
- Atomically updates `is_default` and emits a bus event that invalidates any in-flight query plans.

The old index is **not deleted** on flip. It remains queryable as fallback for the configured grace period (default 30 days, configurable). Decommissioning happens via a separate `ctxt embeddings deprecate` command that schedules removal.

### Query path (this phase)

The query path reads `model_id` from the registry's `is_default = 1` row at startup, caches it, and uses that exclusively. No per-query routing logic. No score fusion. The `embeddings` table's other rows exist as data the migration job populates and the next default-flip will activate — but they're not queried.

This is the explicit "design-for, don't-build" line: the table is multi-model, the query path is single-default. The day a real heterogeneous-corpus or sovereignty use case lands, the query path can grow routing logic without a data-layer migration.

### Operator-facing CLI surface (added to ADR-070's `ctxt upgrade *` subtree)

- `ctxt embeddings list` — registered models, coverage %, default flag, deprecation status
- `ctxt embeddings register <model_id>` — add a candidate; opens the model's config in `$EDITOR` (or `--config <path>`)
- `ctxt embeddings migrate --to <model_id>` — kick off a background migration job
- `ctxt embeddings set-default <model_id>` — atomic default-flip with coverage + recall guards
- `ctxt embeddings deprecate <model_id> --on <date>` — schedule retirement
- `ctxt embeddings purge <model_id>` — actually delete embedding rows for a deprecated model (post-grace-period)

All commands surface progress via the ADR-070 `ctxt upgrade status` infrastructure. Migration jobs are bus-event-emitting per ADR-070 conventions.

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
