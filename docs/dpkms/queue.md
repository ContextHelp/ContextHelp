# Queue System

The queue system in ContextHelp is responsible for executing background work outside the critical ingestion path.

It enables pipelines, registries, and agents to schedule tasks that may require additional processing time, external requests, or batch-style computation without delaying the core user-facing operations (CLI, API, or UI).

The queue system is also home to the **transactional outbox**, which guarantees that ingestion is durable even if the engine crashes during pipeline execution.

The queue is designed around ContextHelp’s foundational principles:

- **Local-first**

- **Decentralized**

- **Deterministic**

- **Resumable & crash-safe**

- **Configurable & extensible**

Below is the updated design reflecting the transactional outbox pattern, job lifecycle semantics, and separation of responsibilities between ingestion and write paths.

---

# Overview

ContextHelp uses a **multi-queue architecture** with a transactional job store:

- All ingestion requests **first write a Job** to persistent storage.
- Pipelines run **only inside workers**, never directly in CLI/API request threads.
- Jobs are processed in **priority-based queues** using FIFO ordering.
- If the engine crashes, jobs remain safely stored and will resume on restart.

This ensures that ingestion is **non-blocking**, **safe**, and **resumable**.

---

# Transactional Outbox Model

Ingestion follows a two-phase pattern:

1. **Phase 1 — Write Job Intent**
   A new job is written to the `jobs` table with status `pending`.
   This write is small, atomic, and never involves AI calls.

2. **Phase 2 — Pipeline Execution in Worker**
   A background worker picks up the job, executes its pipeline(s), and writes the resulting bookmark(s) to storage.

If a crash happens during Phase 2, the job will be retried safely.

This approach prevents partial ingestion, ensures durability, and avoids mixing pipeline execution with synchronous user actions.

---

# Queues & Priorities

ContextHelp defines **three built-in priority queues**:

## High Priority

- Required post-processing
- Metadata normalization
- Canonical indexing operations
- Any work needed before a bookmark becomes usable

## Normal Priority

- Optional enrichment
- Additional tagging passes
- Relationship extraction
- Recomputing derived fields

## Low Priority

- Background metadata fetching (e.g., GitHub / registries)
- Batch rescoring or re-embedding
- Registry synchronization
- Cleanup jobs

Higher-priority queues drain before lower-priority ones.

---

# Jobs

A **job** is the atomic unit of asynchronous work.

Each job includes:

- **id** – unique identifier
- **type** – job class (e.g., `pipeline.run`, `github.fetch`, etc.)
- **payload** – JSON-serializable data needed to execute
- **priority** – high / normal / low
- **status** – `pending`, `running`, `completed`, `failed`, `aborted`
- **attempts / maxAttempts** – retry counters
- **createdAt / updatedAt / startedAt / finishedAt**
- **lastError** – machine-readable failure information

Jobs are stored in a persistent job table to survive crashes.

---

# Job Steps (Optional)

Complex pipelines may emit **job steps** used for:

- progress reporting
- debugging
- high-fidelity audit trails

Each step is small and append-only, e.g.:

```
pipeline.text.short.parse
pipeline.text.short.classify
pipeline.text.short.generateSummary
pipeline.text.short.writeBookmark
```

Workers write steps as they progress; steps are safe to replay or inspect.

---

# Job Lifecycle

1. **Pending**
   Job is created and stored. Awaiting worker.

2. **Running**
   Worker has locked the job and started execution.

3. **Completed**
   Job finished successfully.

4. **Failed**
   Error occurred, but job can be retried.

5. **Aborted**
   Max attempts reached or explicitly cancelled.

Job transitions are atomic and backend-controlled.

---

# Retry Logic

Retries follow bounded exponential backoff:

- Attempt count increments
- Delay = base × (factor ^ attempts)
- Job is requeued at the same priority
- When `maxAttempts` is exceeded, job becomes `aborted`

Failures are logged and optionally surfaced through CLI or monitoring integrations.

---

# Queue Backends

The queue service is pluggable.

## In-Memory Queue (default)

- Zero configuration
- Volatile (loses state on restart)
- Ideal for dev/testing

## SQLite-Backed Queue (recommended)

- Persistent across restarts
- Safe for concurrency in WAL mode
- Handles crashes reliably
- Optimal for the default local-first deployment

## External Queue Providers (optional)

- Redis
- NATS
- Postgres-backed job tables

External queues are optional and never required for personal use.

---

# Workers

Workers are responsible for executing jobs.

Characteristics:

- Lightweight goroutines
- One worker pool per priority class
- Pull–execute–report cycle
- Panic isolation
- Graceful shutdown supported

Example config:

```yaml
queue:
  workers:
    high: 4
    normal: 2
    low: 1
```

Workers only mutate bookmark storage after a successful pipeline run, maintaining strong separation between ingestion and write paths.

---

# Ingestion Path Integration

Ingestion (`ch analyze`) behaves as follows:

1. Validate input
2. Normalize and prepare payload
3. Write a **pipeline.run** job to the queue
4. Return Job ID immediately
5. Workers process job and write bookmark(s)

Users may optionally pass `--wait` to block until a job completes.

No bookmark is written directly from the CLI.

---

# Queue Interaction in Pipelines

Pipelines can enqueue:

- follow-up enrichment jobs
- metadata lookups
- reprocessing jobs
- dependency jobs for multi-step analysis

Each job must be deterministic and idempotent.

Example:

```go
queue.Enqueue(Job{
  Type: "github.metadata.fetch",
  Payload: {...},
  Priority: queue.Low,
  MaxAttempts: 3,
})
```

Pipelines may also inspect job context via injected interfaces.

---

# Queue Interaction in Registries

Registries use jobs for:

- periodic taxonomy refresh
- index maintenance
- augmentation tasks
- remote metadata expansion

Registry jobs never block normal queries.

---

# CLI & API Surface

The queue exposes its own control plane.

CLI:

```
ch queue list
ch queue view <id>
ch queue retry <id>
ch queue abort <id>
ch queue drain
```

REST API:

- `GET /queue/jobs`
- `GET /queue/jobs/{id}`
- `POST /queue/jobs/{id}/retry`
- `POST /queue/jobs/{id}/abort`

gRPC:

- `ListJobs`
- `GetJob`
- `RetryJob`
- `AbortJob`

---

# Concurrency & Safety

To ensure integrity:

- SQLite-backed queue uses WAL mode for safe concurrent readers/writer model
- Workers lock jobs atomically during execution
- Multi-process safety is supported when using SQLite or external providers
- Workers serialize jobs on the same bookmark if configured
- Panics are intercepted and logged without killing the worker pool

---

# Configuration

Example:

```yaml
queue:
  backend: "sqlite"
  directory: "~/.contexthelp/queue"
  workers:
    high: 4
    normal: 2
    low: 1
  maxAttempts:
    default: 5
    github.metadata.fetch: 3
    pipeline.run: 2
  serialization:
    perBookmark: true
```

All fields are optional; safe defaults apply.

---

# Future Extensions

- Recurring jobs (cron-style)
- Distributed multi-node processing
- Queue metrics and dashboard
- Pluggable serializers for payloads
- Sandboxed job execution
- GPU-aware workers for local LLM integrations

---

# Summary

The queue system is the backbone of ContextHelp's durability and responsiveness.

It provides:

- Safe ingestion via transactional outbox
- Fully asynchronous pipeline execution
- Crash recovery and resume capabilities
- Priority-based local-first queues
- Extensible job orchestration for pipelines and registries

This design enables ContextHelp to scale in sophistication while maintaining local-first reliability and decentralized operation.