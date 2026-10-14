# Jobs and Ingestion

This document defines the ingestion architecture, job lifecycle, execution model, crash-recovery guarantees and worker behavior for ContextHelp.
It complements the minimal ADR set—particularly **ADR-003 (Separate Write/Read Paths)**, **ADR-007 (Transactional Outbox Pattern)**, and **ADR-078 (No Plugin System)**—and provides the implementation blueprint for robust, fault-tolerant, and extensible analysis workflows.

---

## Overview

In ContextHelp, **no ingestion or long-running operation executes inline inside a CLI or API call**.
Instead, all ingestion proceeds through a **durable, observable job system** that guarantees:

- crash safety
- retry safety
- deterministic pipeline behavior
- resumability and replay
- introspection and debuggability
- consistent storage writes

Every capture—text, URL, image, audio, video, feed item—follows this sequence:

1. **A user or a capture source triggers ingestion** (CLI, REST, gRPC, file watcher, feed).
2. The engine creates a **Job** with status `pending`, storing all inputs.
3. The background worker (`ch serve`) picks up pending jobs.
4. The worker executes the appropriate **pipeline**, built-in or custom.
5. Pipeline steps produce outputs.
6. Worker writes a new **Bookmark** entry.
7. Job transitions to `completed` (or `failed` with retry logic).

This pattern ensures ingestion remains reliable, isolated, extensible, and safe.

---

## Job Lifecycle

A job moves through the following states:

- **pending**
  Newly created, awaiting execution.

- **running**
  Worker has locked the job and is executing pipeline steps.

- **completed**
  Bookmark or output successfully created and job finalized.

- **failed**
  Job exceeded retry limits or encountered an unrecoverable error.

- **retrying** (implicit)
  A transient error occurred; the job was returned to `pending`.

The lifecycle mirrors the transactional outbox pattern and is intentionally simple.

---

## Job Store Schema

Jobs are persisted in durable local storage—typically SQLite—before any processing occurs.

### `jobs` table

| Column            | Type        | Description |
|-------------------|-------------|-------------|
| `job_id`          | TEXT (PK)   | Unique ID |
| `status`          | TEXT        | `pending`, `running`, `completed`, `failed` |
| `payload`         | JSON        | Raw input: type, hints, file reference, content |
| `pipeline`        | TEXT        | Pipeline name (e.g. `text.short`, `feed.fetch`, custom) |
| `retry_count`     | INTEGER     | Number of retry attempts |
| `max_retries`     | INTEGER     | Configured globally or per-pipeline |
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
| `step`       | TEXT      | Pipeline step name |
| `status`     | TEXT      | `pending`, `running`, `completed`, `failed` |
| `log`        | TEXT      | Any debug or warnings |
| `started_at` | DATETIME  | Start time |
| `ended_at`   | DATETIME  | End time |

This enables introspection and debugging.

---

## Ingestion Flow

### Step 1: Ingestion Trigger

Job creation may come from:

- CLI (`ch analyze`)
- REST / gRPC
- Browser extension
- Feed auto-fetch
- Scheduled refresh

Regardless of source, ingestion **never executes immediately**, ensuring a unified and safe execution model.

### Step 2: Create Job (Transactional Outbox)

The engine atomically stores:

- input type
- raw content or URL
- metadata (hints, tags, mentions)
- pipeline name
- timestamps

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

Pipeline execution is deterministic:

- inference step
- fetch step
- parsing step
- summarization
- tagging
- mention extraction
- entity resolution
- installed steps (from a step registry)
- enrichment steps

Each step may:

- produce output
- emit events
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
- step failures
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

---

## Job Introspection (Observability)

Tools for observing pipeline behavior:

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

---

## Interaction With Pipelines

### Pipelines Do Not Run Inline

All processing happens inside worker context.

### Pipelines Do Not Write Bookmarks Directly

The worker writes the final output once pipeline execution succeeds.

### Pipelines May Enqueue Jobs

This is essential for:

- RSS feed parsing → enqueue feed-item ingestion
- Refresh → enqueue refresh jobs

---

## Interaction With Storage

Storage must provide:

- atomic job creation
- safe job claiming
- bookmark writes independent of job writes
- crash-safe journaling

SQLite is default; Postgres is the other supported driver.

---

## Advanced: Delayed, Scheduled, and Background Jobs

The ingestion system supports delayed and scheduled execution, enabling:

- recurring feed refresh
- nightly cleanup routines
- periodic registry sync

These operations are expressed as **jobs** and use the exact same lifecycle.

---

## Summary

The jobs and ingestion system provides:

- **Safety** (crash tolerance, durable writes)
- **Determinism** (idempotent pipelines and replayable jobs)
- **Deduplication** (content-hash based, with reinforcement tracking on re-ingestion)
- **Extensibility** (custom pipelines and installed steps run on the same job model)
- **Observability** (job_steps logs, worker inspection)
- **Scalability** (multi-worker support with concurrent race handling)
- **Correctness** (clear separation of write/read paths)

It is the backbone of ContextHelp's ingestion, keeping every ingestion robust, predictable, and safe.
