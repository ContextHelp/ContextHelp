# Jobs and Ingestion

This document defines the ingestion architecture, job lifecycle, execution model, crash-recovery guarantees, worker behavior, and **plugin integration points** for ContextHelp.
It complements the minimal ADR set—particularly **ADR-003 (Separate Write/Read Paths)**, **ADR-007 (Transactional Outbox Pattern)**, and **ADR-012 (Plugin Extensibility)**—and provides the implementation blueprint for robust, fault-tolerant, and extensible analysis workflows.

---

## Overview

In ContextHelp, **no ingestion or long-running operation executes inline inside a CLI or API call**.
Instead, all ingestion proceeds through a **durable, observable job system** that guarantees:

- crash safety
- retry safety
- deterministic pipeline behavior
- resumability and replay
- plugin-triggered job creation
- introspection and debuggability
- consistent storage writes

Every capture—text, URL, image, audio, video, feed item, plugin-derived task—follows this sequence:

1. **User or Plugin triggers ingestion** (CLI, REST, gRPC, file watcher, plugin event).
2. The engine creates a **Job** with status `pending`, storing all inputs.
3. The background worker (`ch serve`) picks up pending jobs.
4. The worker executes the appropriate **pipeline**, including plugin-defined ones.
5. Pipeline steps (and plugin hooks) produce outputs.
6. Worker writes a new **Bookmark** entry, or plugin-specific result.
7. Job transitions to `completed` (or `failed` with retry logic).

This pattern ensures ingestion remains reliable, isolated, extensible, and safe.

---

## Job Lifecycle

A job moves through the following states:

- **pending**
  Newly created, awaiting execution (created by user or plugin).

- **running**
  Worker has locked the job and is executing pipeline steps.

- **completed**
  Bookmark or output successfully created and job finalized.

- **failed**
  Job exceeded retry limits or encountered an unrecoverable error.

- **retrying** (implicit)
  A transient error occurred; the job was returned to `pending`.

The lifecycle mirrors the transactional outbox pattern and is intentionally simple, enabling both internal and plugin-originated jobs.

---

## Job Store Schema

Jobs are persisted in durable local storage—typically SQLite—before any processing occurs.

### `jobs` table

| Column            | Type        | Description |
|-------------------|-------------|-------------|
| `job_id`          | TEXT (PK)   | Unique ID |
| `status`          | TEXT        | `pending`, `running`, `completed`, `failed` |
| `payload`         | JSON        | Raw input: type, hints, plugin data, file reference, content |
| `pipeline`        | TEXT        | Pipeline name (e.g. `text.short`, `feed.fetch`, plugin-defined) |
| `retry_count`     | INTEGER     | Number of retry attempts |
| `max_retries`     | INTEGER     | Configured globally, per-pipeline, or by plugin |
| `created_at`      | DATETIME    | Job creation timestamp |
| `updated_at`      | DATETIME    | Last status change |
| `locked_at`       | DATETIME    | Worker lock timestamp |
| `error`           | TEXT        | Last error message, if any |

### `job_steps` table (optional but recommended)

Tracks each step during pipeline execution:

| Column       | Type      | Description |
|--------------|-----------|-------------|
| `id`         | TEXT (PK) | Step record ID |
| `job_id`     | TEXT      | Associated job |
| `step`       | TEXT      | Pipeline or plugin-defined step name |
| `status`     | TEXT      | `pending`, `running`, `completed`, `failed` |
| `log`        | TEXT      | Any debug or warnings |
| `started_at` | DATETIME  | Start time |
| `ended_at`   | DATETIME  | End time |

This enables introspection, debugging, and plugin-managed workflows.

---

## Ingestion Flow

### Step 1: Ingestion Trigger

Job creation may come from:

- CLI (`ch analyze`)
- REST / gRPC
- Browser extension
- Feed auto-fetch (RSS plugin)
- Price drop monitor (plugin)
- Refresh plugin running scheduled ingestion
- Plugin-defined automation events

Regardless of source, ingestion **never executes immediately**, ensuring a unified and safe execution model.

### Step 2: Create Job (Transactional Outbox)

The engine atomically stores:

- input type
- raw content or URL
- metadata (hints, tags, mentions)
- pipeline name or plugin-provided pipeline
- timestamps
- plugin metadata (if any)

If the process crashes at this moment, the job still exists and will run later.

### Step 3: Worker Locks and Executes Job

`ch serve` continuously pulls jobs:

```
SELECT * FROM jobs
WHERE status = 'pending'
ORDER BY created_at ASC
LIMIT 1
FOR UPDATE
```

Job transitions to `running`.

### Step 4: Execute Pipeline Steps

Pipeline execution is deterministic and aware of plugins:

- inference step
- fetch step
- parsing step
- summarization
- tagging
- mention extraction
- entity resolution
- plugin-defined steps (e.g., feed.parse, feed.ingest_items, price.scan)
- enrichment steps

Each step may:

- produce output
- emit plugin events
- enqueue additional jobs
- fail and retry
- write debug logs

Failure semantics:

- **Retryable** errors → return to `pending`
- **Permanent** errors → mark job as `failed`

### Step 5: Dedup Check and Write

After pipeline execution, the worker computes a **content hash** (SHA-256 of normalized content + source) and checks for an existing bookmark with the same hash.

**Duplicate found** — the worker **reinforces** the existing bookmark:
- Merges new tags and mentions (dedup by label/string, first-seen wins)
- Increments `reinforcement_count`
- Updates `last_reinforced_at`
- Completes the job with the existing bookmark's ID

**No duplicate** — the worker creates a new bookmark:
- Sets `content_hash`, `reinforcement_count = 1`
- Writes bookmark and mention edges (ADR-049)

**Concurrent race** — if two workers process the same content simultaneously, the unique index on `content_hash` prevents duplicate creation. The second worker catches the constraint violation and falls back to reinforcement.

This ensures idempotent ingestion: re-ingesting the same content strengthens the signal rather than creating duplicates.

### Step 6: Mark Completed

The job transitions to `completed`.

---

## Crash Recovery

The ingestion system tolerates:

- worker crashes
- CLI/API call interruptions
- plugin failures
- OS termination
- network/AI provider outages

### Stale Job Detection

A job is stale if:

- `locked_at` older than configured threshold
- worker PID no longer active
- host restarted

Stale jobs are returned to **pending** automatically.

This ensures no zombie jobs or pipeline deadlocks.

---

## Retry Semantics

Jobs have `max_retries`:

- global defaults
- per-pipeline settings
- plugin-specified overrides

Retry rules:

- transient errors → retry
- permanent errors → fail immediately
- retry exhaustion → `failed`

### Manual retry:

```bash
ch jobs retry <job_id>
```

---

## Running Multiple Workers

Supports:

### Single Worker (default)
- Ideal for local-first operation.

### Multi-Worker Mode
- Requires shared storage
- Workers coordinate locking
- Plugins remain safe, as job model is concurrency-agnostic

---

## Job Introspection (Observability)

Tools for observing pipeline (or plugin) behavior:

```bash
ch jobs list
ch jobs status <job_id>
ch jobs logs <job_id>
```

Example:

```
Job 9128-ABCD
Status: running
Pipeline: feed.fetch
Step 2/5: rss-parse
Retries: 1/3
Last error: network timeout
```

Plugins benefit from the same introspection surface.

---

## Interaction With Pipelines

### Pipelines Do Not Run Inline

All processing happens inside worker context.

### Pipelines Do Not Write Bookmarks Directly

The worker writes the final output once pipeline execution succeeds.

### Pipelines May Enqueue Jobs

This is essential for:

- RSS feed parsing → enqueue feed-item ingestion
- Refresh plugin → enqueue refresh jobs
- Price monitor plugin → enqueue re-validation jobs
- Any plugin that wants asynchronous actions

This capability is part of the **official plugin API**.

---

## Interaction With Plugins

The job system provides formal guarantees enabling safe plugin operation:

### Plugins Can:
- enqueue new jobs
- define new pipelines
- define new pipeline steps
- read job payloads
- add metadata to job payloads
- declare retryability
- produce outputs stored alongside pipeline results
- register for `post_ingest`, `post_pipeline_step`, `post_refresh` hooks

### Plugins Cannot:
- modify existing jobs belonging to other plugins
- mutate core tables directly
- bypass locking or retry semantics

Plugins operate strictly through stable APIs advertised by core.

---

## Interaction With Storage

Storage must provide:

- atomic job creation
- safe job claiming
- bookmark writes independent of job writes
- plugin storage isolation (plugins write JSON into their own directories)
- crash-safe journaling

SQLite is default; Postgres or plugin-defined stores can be plugged in.

---

## Advanced: Delayed, Scheduled, and Background Jobs

The ingestion system supports delayed and scheduled execution, enabling:

- recurring feed refresh (RSS plugin)
- price re-checks (Price Monitor plugin)
- nightly cleanup routines
- periodic registry sync
- plugin-defined tasks

These operations are expressed as **jobs** and use the exact same lifecycle.

Plugins do not require special treatment—they simply enqueue delayed jobs via the plugin API.

---

## Summary

The jobs and ingestion system provides:

- **Safety** (crash tolerance, durable writes)
- **Determinism** (idempotent pipelines and replayable jobs)
- **Deduplication** (content-hash based, with reinforcement tracking on re-ingestion)
- **Extensibility** (plugins can enqueue jobs and define pipelines)
- **Observability** (job_steps logs, worker inspection)
- **Isolation** (plugin jobs never interfere with core jobs)
- **Scalability** (multi-worker support with concurrent race handling)
- **Correctness** (clear separation of write/read paths)

It is the backbone of ContextHelp's ingestion and the enabling layer for a rich plugin ecosystem, ensuring all ingestion—core or plugin-driven—remains robust, predictable, and safe.