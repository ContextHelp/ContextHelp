# Operate Embedding Models

Use this page to run the embedding models behind semantic search on a dpkms instance: register a model, fill its vectors, make it the default, switch to another one, and retire the old one. It also covers the notice `ctxt find` prints when it falls back to full-text search, and near-duplicate detection, which runs on the same vectors.

Setting up semantic search for the first time is shorter: [Turn on semantic search](../workflows/semantic-search.md).

## How it fits together

| Command | What it changes |
|---|---|
| `ctxt embeddings register <id>` | Adds a model. From now on every new capture is embedded with it too. |
| `ctxt embeddings migrate --to <id>` | Queues a background job in dpkms that embeds existing objects missing a vector for the model. |
| `ctxt embeddings set-default <id>` | Makes the model the one queries use, once it covers enough objects. |
| `ctxt embeddings deprecate <id>` | Stops embedding new captures with the model from a date on. |
| `ctxt embeddings purge <id>` | Deletes a deprecated model's vectors, index and registry entry after a grace period. |
| `ctxt embeddings list` / `provider` | Read-only: models with their coverage; the provider a command resolves to. |

Every registered model that isn't deprecated is embedded on every capture, so each one costs one provider call per capture. Deprecate models you are done with.

The `ctxt embeddings` commands open the instance's database directly: `storage.path`, or the database of the instance named by `--instance` or `ctxt instance use`. Run them where that database can be opened. Only the `migrate` job itself runs in dpkms.

## Switch to another model

This walks an instance from `ollama-nomic-embed-text@2026-09-26` (the current default) to `ollama-snowflake-arctic-embed2@2026-09-26`. Search keeps using the old model until the flip in step 4.

**1. Register the new model.** Name the model with a flag, so the config file's provider stays as it is:

```bash
ctxt embeddings register ollama-snowflake-arctic-embed2@2026-09-26 \
    --embedding-model snowflake-arctic-embed2 --dimension 1024
```

```text
Registered ollama-snowflake-arctic-embed2@2026-09-26

╭─────────────┬─────────────────────────┬──────────╮
│ Setting     │ Value                   │ Source   │
├─────────────┼─────────────────────────┼──────────┤
│ backend     │ ollama                  │ default  │
│ model       │ snowflake-arctic-embed2 │ flag     │
│ endpoint    │ http://localhost:11434  │ default  │
│ api_key_env │ (unset)                 │ default  │
│ dimension   │ 1024                    │ measured │
╰─────────────┴─────────────────────────┴──────────╯
Index: ready
Not the default model. Promote it with: ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26
```

New captures are now embedded with both models.

**2. Fill its vectors for existing objects.**

```bash
ctxt embeddings migrate --to ollama-snowflake-arctic-embed2@2026-09-26
```

**3. Wait for coverage.** Follow the job with `ctxt upgrade status --watch` ([Follow a migration](#follow-a-migration)), then confirm with `ctxt embeddings list`:

```text
Embedding models (2)

╭───────────────────────────────────────────┬──────────┬──────┬─────────┬──────────┬──────────────────────┬────────────╮
│ Model ID                                  │ Provider │ Dim  │ Default │ Coverage │ Registered           │ Deprecated │
├───────────────────────────────────────────┼──────────┼──────┼─────────┼──────────┼──────────────────────┼────────────┤
│ ollama-nomic-embed-text@2026-09-26        │ ollama   │ 768  │ *       │ 1.00     │ 2026-09-26T19:55:53Z │            │
│ ollama-snowflake-arctic-embed2@2026-09-26 │ ollama   │ 1024 │         │ 1.00     │ 2026-09-26T19:56:26Z │            │
╰───────────────────────────────────────────┴──────────┴──────┴─────────┴──────────┴──────────────────────┴────────────╯
```

**4. Flip the default.** Preview with `--dry-run`, then run it:

```bash
ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26 --dry-run
ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26
```

```text
Default embedding model is now ollama-snowflake-arctic-embed2@2026-09-26 (was ollama-nomic-embed-text@2026-09-26).
Coverage: 1.0000 (minimum 0.9900)
The previous default keeps receiving vectors until deprecated: ctxt embeddings deprecate ollama-nomic-embed-text@2026-09-26
```

The next query in any process, dpkms included, uses the new model. Nothing needs a restart. The old model keeps its vectors, so flipping back is another `set-default`, as long as its coverage is still above the minimum.

**5. Stop writing the old model.** Deprecation is guarded by a typed token. Run the command once to get the token, then again with it:

```bash
ctxt embeddings deprecate ollama-nomic-embed-text@2026-09-26
ctxt embeddings deprecate ollama-nomic-embed-text@2026-09-26 --confirm-token <token>
```

```text
Deprecated ollama-nomic-embed-text@2026-09-26 from 2026-09-26T19:56:48Z: ingest stops writing its vectors then.
Purge allowed from 2026-10-26T19:56:48Z (grace period 720h0m0s): ctxt embeddings purge ollama-nomic-embed-text@2026-09-26
```

**6. Purge it after the grace period** (30 days by default):

```bash
ctxt embeddings purge ollama-nomic-embed-text@2026-09-26 --confirm-token <token>
```

```text
Purged ollama-nomic-embed-text@2026-09-26: deleted 30 embedding rows, its index and its registry entry.
```

Purging can't be undone. Until then the old model is your way back.

## Commands

### `register`

```bash
ctxt embeddings register <model_id> [--embedding-provider B] [--embedding-model M] [--embedding-endpoint URL] [--dimension N]
```

- The provider is resolved like every embedding command: flags, `CTXT_EMBEDDING_*`, `-c providers.embedding.*`, the config file, the defaults. Preview it with `ctxt embeddings provider` and the same flags.
- Register embeds one fixed string and records the vector's length as the model's dimension. If the provider can't be reached, nothing is registered.
- `--dimension N` is a check: when the measured dimension differs, the command fails with exit code 4 and registers nothing. `providers.embedding.dimension` in config (or `-c`) is checked the same way when it applies to the model being registered; `--dimension` wins when both are set.
- The registry entry stores the backend, model, endpoint and `api_key_env` (the variable's name, never the key). Backend and model are fixed from then on; endpoint and `api_key_env` stay overridable per run.
- A model ID may use letters, digits and `. _ : @ / + -`, must start with a letter or digit, and has at most 200 characters. It names one vector space: to register a different model, use a new ID, such as a later date.
- The dimension limit is 8192 on SQLite and 2000 on Postgres.
- Registering never changes the default. It builds the model's vector index right away (`Index: ready`).
- `--dry-run` resolves the provider, probes the dimension and runs every check a real run does, then prints what would be registered. It writes nothing: no registry entry, no index. With `--format json` the output has `"dry_run": true` and `"index": "skipped"`.

### `list`

`ctxt embeddings list` shows every model: dimension, the default (`*`), coverage, and when it was registered and deprecated. Coverage is the fraction of objects with a vector for the model; an empty database reports `1.00`. `--format json` gives the same fields for scripts.

### `provider`

`ctxt embeddings provider [model_id]` prints the backend, model, endpoint, `api_key_env` and dimension a command would use, with the layer each came from. It accepts the same `--embedding-*` flags, env and `-c` as the other commands and never prints a key. With a `model_id` it resolves the way a command targeting that model does: backend, model and dimension show as "fixed by registry". Details: [Check what a command will use](../../environment-variables/ai-providers.md#check-what-a-command-will-use).

### `migrate`

```bash
ctxt embeddings migrate --to <model_id> [--rate-limit N/s] [--batch N] [--dry-run]
```

- Queues an `embeddings:migrate` job for the instance's dpkms. The job embeds every object that has no vector for the model, using only that model's provider. If no dpkms is running, the job starts when dpkms does.
- It keeps going after `ctxt` exits and resumes after a dpkms restart. A re-run never embeds an object twice, since it only looks at objects still missing a vector.
- `--rate-limit` caps provider calls per second (default: no cap). `--batch` sets objects per page (default 100, at most 1000).
- `--dry-run` prints how many objects lack a vector and queues nothing.
- While a job for the model is queued or running, another `migrate` reports it and queues nothing. A model that is complete queues nothing.
- dpkms applies its own runtime overrides to the job. To run it against a remote Ollama, start dpkms with `CTXT_EMBEDDING_ENDPOINT=http://127.0.0.1:11555`.
- There is no cost cap (`--budget-usd`) and no pause. To stop a run, cancel the job; to continue, run `migrate` again.

#### Follow a migration

```bash
ctxt upgrade status
```

```text
Upgrade state: in_progress
  Bucket:    embeddings_migrate
  Target:    ollama-snowflake-arctic-embed2@2026-09-26
  Progress:  9/30 (30%)
  ETA:       6s
  Started:   2026-09-26T19:56:26Z
```

- `ctxt upgrade status` reads the configured instance: the first `server.urls` entry, else `server.url`, with its token. Pass `--server` for any other instance. It exits 70 when nothing answers there.
- `--watch` refreshes until the state is idle. `--format json` returns the `/healthz` upgrade envelope: `state`, `bucket` (`embeddings_migrate`), `target` (the model ID), `done`, `total`, `failed`, `eta_seconds`, `last_error`.
- While a job runs, every `ctxt` command prints a one-line `ℹ ctxt: upgrading embeddings_migrate; …` banner on stderr.
- When a run ends it goes back to `idle`, unless objects failed.

#### When objects fail

An object the provider rejects is counted, skipped, and left for the next run. A run that ends with failures, or is cancelled, leaves the state at `failed`, and `ctxt upgrade status` exits 1:

```text
Upgrade state: failed
  Bucket:    embeddings_migrate
  Target:    ollama-nomic-embed-text@2026-09-26
  Progress:  30/30
  Failed:    30 objects
  Last error: 30 of 30 objects not embedded under ollama-nomic-embed-text@2026-09-26; re-run `ctxt embeddings migrate --to ollama-nomic-embed-text@2026-09-26` to retry them (first: 033b3eca-dac3-4ac0-b3e7-7383fa3a29cc: embed: ollama embed: Post "http://127.0.0.1:11555/api/embeddings": dial tcp 127.0.0.1:11555: connect: connection refused)
```

The `failed` state stays until the next `migrate` run starts. Fix the cause (here dpkms reached Ollama through a tunnel that was down), then run the same `migrate` again: it retries exactly the objects still missing.

#### Stop a migration

```bash
dpkms job list --server-url http://localhost:8080
dpkms job cancel <job-id> --server-url http://localhost:8080 --confirm yes
```

Outside a terminal, `cancel` refuses without `--confirm yes`. The run stops after the object in flight and the status turns `failed` ("stopped after 22 of 30 objects"). Vectors written so far are kept; `ctxt embeddings migrate --to <model_id>` continues from there.

### `set-default`

```bash
ctxt embeddings set-default <model_id> [--min-coverage F] [--dry-run]
```

- Refused unless the model's coverage is at least `embeddings.min_coverage` (default `0.99`). `--min-coverage` overrides it for one call, from `0` to `1`.
- A deprecated model, or one that isn't registered, is refused too.
- A refusal exits with code 4 and changes nothing: `CONFLICT: set-default: <id> covers 0.00 of the corpus, below the minimum 0.99; the default is unchanged`.
- The flip is one transaction: there is never more than one default, and a concurrent flip waits. Promoting the current default is a no-op.
- There is no recall check before the flip; compare results yourself before flipping.

Coverage drifts down as new objects arrive that no embedding pipeline touches, such as entity pages. Before a flip, run `migrate --to` for the candidate once more.

### `deprecate`

```bash
ctxt embeddings deprecate <model_id> [--on DATE] --confirm-token <token>
```

- From the date on, new captures are no longer embedded with the model, and it can't become the default. Its vectors and index stay until `purge`.
- `--on` takes `2026-10-01` (start of that day, local time), a local date-time `2026-10-01T09:00:00`, or RFC 3339 `2026-10-01T09:00:00Z`. Without it, the deprecation takes effect now. A date in the past is refused, because it would shorten the grace period.
- The default model can't be deprecated. Promote its successor first.
- Deprecating again without `--on` keeps the date; with `--on` it reschedules.
- `--dry-run` shows the effective date and when purge becomes possible, without the token.

### `purge`

```bash
ctxt embeddings purge <model_id> --confirm-token <token>
```

- Deletes the model's vectors, its vector index, its index signature and its registry entry. This can't be undone.
- Refused, deleting nothing, unless the model is deprecated and `embeddings.grace_period` (default `720h`, 30 days) has passed since the deprecation took effect. The default model is never purged.
- To purge sooner, shorten the grace period for one run: `-c embeddings.grace_period=0s`.
- `--dry-run` reports how many vectors would be deleted.

- A refusal exits with code 4.

`deprecate` and `purge` print their token when run without one.

## Search fallback notice

A query that can't use the default model's index still answers from full-text search, and `ctxt find` says so on stderr:

```text
notice: semantic search unavailable (<status>): <detail>; results are full-text only
```

With `--format json` the same report is under `diagnostics.semantic` (`status`, `model_id`, `detail`, `notice`).

| Status | Meaning | Fix |
|---|---|---|
| `ok` | The semantic leg ran; no notice. | — |
| `no_default_model` | No model is the default: none registered yet, or none promoted. | `register`, `migrate --to`, `set-default`. |
| `provider_error` | The default model's provider failed: unreachable, no vector returned, or an override tried to change its backend or model. | `ctxt embeddings provider <default id>` shows what was used; fix the endpoint or drop the override. |
| `dimension_mismatch` | The provider returned vectors of another length than the model's registered dimension; a different model is answering at that endpoint. | Point the endpoint at the registered model, or register the new one under a new ID and switch. |
| `index_missing` | The default model has no measured dimension, or has no vector index even though opening the database rebuilds every missing index. | No index: the open skipped the model and logged why just before the notice (`embedding index skipped`), usually stored vectors whose length differs from the registered dimension. Restarting doesn't help. For either cause, register the model under a new model_id (register measures the dimension), `migrate --to` it, then `set-default`. |
| `low_coverage` | The default model's index holds no vectors at all. | `ctxt embeddings migrate --to <default id>`. |

With `search.fallback_to_fts: false`, `find` fails instead (exit code 70) when the status isn't `ok`. The default is `true`.

## Duplicates

Duplicate detection runs in dpkms at capture time, so its settings go in `dpkms.yaml`:

```yaml
duplicates:
  policy: warn              # warn | keep | drop
  check_exact: true         # same content hash or source key
  check_similar: false      # near-duplicates by vector similarity
  similarity_threshold: 0.95
```

- **Exact duplicates** (same content hash or source key) are checked when content is submitted, before a job is queued.
- **Near-duplicates** are off by default. With `check_similar: true`, a `dedup` step runs right after the embedding step in every pipeline that embeds. It compares the new object's vector under the default model with that model's index. A cosine similarity at or above `similarity_threshold` makes it a near-duplicate. Without a default model the check is skipped.

What each policy does:

| Policy | Exact duplicate | Near-duplicate |
|---|---|---|
| `warn` (default) | Queued as usual; the server logs a warning. | Stored and recorded on the new object; warning logged. |
| `keep` | Queued as usual. | Stored and recorded on the new object. |
| `drop` | Not queued; submitting answers with the existing object's ID. | Not stored; the job completes with the existing object's ID. dpkms logs `INFO jobs: near-duplicate dropped, not stored job=… duplicate_of=…`. |

A near-duplicate is recorded in the new object's metadata. dpkms logs `WARN dedup: near-duplicate detected object=… duplicate_of=… similarity=… model_id=…` under `warn`.

Every dedup decision (exact or near) is also written to the audit log as `dedup.exact` or `dedup.similar`, with the duplicate's ID, similarity, policy and model, and no content. List them with `ctxt audit list --event-type dedup.similar`.

```bash
curl -s http://localhost:8080/api/v1/objects/<id> | jq '.metadata | {duplicate_of, duplicate_similarity, duplicate_kind}'
```

```json
{
  "duplicate_of": "fad40b89-0e1d-4339-a205-dca4958b126b",
  "duplicate_similarity": 0.9898723559454083,
  "duplicate_kind": "similar"
}
```

To turn the near-duplicate check off for one pipeline while keeping it elsewhere, skip its `dedup` step:

```yaml
pipelines:
  overrides:
    text.short:
      skip_steps: [dedup]
```

## Settings

| Key | Default | Used by |
|---|---|---|
| `providers.embedding.backend` / `model` / `endpoint` / `api_key_env` | `ollama` / `nomic-embed-text` / `http://localhost:11434` / unset | `register`, and `find` / ingest when no model is registered ([reference](../../environment-variables/ai-providers.md#embedding-provider)) |
| `providers.embedding.dimension` | unset | Shown by `provider`; not checked by `register` |
| `embeddings.min_coverage` | `0.99` | `set-default` |
| `embeddings.grace_period` | `720h` | `purge` |
| `duplicates.policy` / `check_exact` / `check_similar` / `similarity_threshold` | `warn` / `true` / `false` / `0.95` | Capture in dpkms |
| `search.fallback_to_fts` | `true` | `find` when the semantic leg can't run |

A per-pipeline `pipelines.overrides.<name>.providers.embedding` is ignored with a warning: each registered model's own entry decides its provider.

## Known limitations

- Two dpkms processes serving one Postgres database can both run the same migration job. Rows stay correct, but provider calls are wasted. Run one dpkms per database while migrating.

## Related

- [Turn on semantic search](../workflows/semantic-search.md)
- [Embedding provider settings](../../environment-variables/ai-providers.md#embedding-provider)
- [Runbook](./runbook.md)
