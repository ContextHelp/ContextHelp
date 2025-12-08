# ADR-007 – Implement Ingestion Using a Transactional Outbox (Jobs Table)

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp processes diverse content inputs (text, URLs, images, audio, video) and runs them through pipelines that may call multiple AI providers, perform transformations, fetch metadata, or extract structured representations. These operations:

- may take seconds to minutes
- may intermittently fail (timeout, OOM, rate limits)
- may be canceled by the user (closing the terminal)
- may be interrupted by system shutdowns
- may require retries or partial re-execution

Until now, the CLI (`ch analyze`) executed pipelines synchronously. If the process crashed or stalled mid-execution:

- the partially processed item would be lost
- no bookmark would be created
- no retry would occur
- the system could enter inconsistent states
- long operations blocked the CLI, harming UX

Additionally, future features—including background ingestion, metadata enrichment, incremental pipelines, continuous sync, agent-triggered processing, and scheduled tasks—require a reliable job execution mechanism.

Constraints:

- Local-first system → must survive crashes without external infra
- Decentralized registries → ingestion sometimes requires chained operations
- Reliability → no silent failures allowed
- UX → CLI must remain responsive
- Privacy → no data may leave the user's device unintentionally
- Extensibility → plugins must be able to register their own job types

Affected subsystems:

- CLI (`analyze`)
- Pipelines (all types)
- Storage layer
- Queue/worker subsystem
- APIs (REST/gRPC)
- Future background services

The goal is to ensure **durable, resumable, auditable ingestion** across all pipelines.

---

## Decision

**Ingestion will be implemented using a transactional outbox pattern: `ch analyze` writes a job to a persistent `jobs` table, and a background worker processes pending jobs reliably until completion.**

---

## Rationale

### Why transactional outbox?

A transactional outbox cleanly separates:

- **enqueueing** (fast, durable, synchronous)
- **execution** (slow, asynchronous, retryable)

This ensures that ingestion does not depend on the lifetime of the CLI process. It enables robust job recovery if:

- the CLI crashes
- the worker crashes
- the OS kills the process
- AI providers fail mid-completion

### Alternative Approaches & Why Rejected

**1. Synchronous pipeline execution (previous model)**
Rejected because:
- fragile under crashes
- CLI becomes unresponsive
- future parallelization impossible
- cannot support background or scheduled jobs
- no retry semantics

**2. In-memory queue**
Rejected because:
- disappears on crash
- inconsistent with local-first architecture
- no persistence guarantees

**3. External queue (Redis, NATS, etc.)**
Rejected because:
- violates local-first principle
- increases operational complexity
- unnecessary for single-user environments

### Benefits of the chosen approach

- Crash-safe ingestion
- Reliable, resumable pipeline execution
- Worker separation allows parallelism and controlled resource use
- Enables upcoming features:
  - background GitHub metadata fetch
  - incremental enrichment
  - continuous document monitoring
  - agent-triggered jobs
- Aligns with decentralized design (registries may trigger jobs)
- Clean system boundaries: write path → durable → read path

### Risks or Drawbacks

- Introduces a more complex job management layer
- Multi-worker scaling requires careful locking
- Requires new CLI commands to introspect job states
- Must prevent job storms (runaway retries)

---

## Consequences

### Positive

- **Highly reliable ingestion** even under failure conditions.
- **Non-blocking CLI** → `ch analyze` becomes instant.
- **Foundation for background/parallel work**.
- **Deterministic and auditable execution** with `job_steps`.
- **Supports plugin-defined jobs** without architectural changes.

### Negative

- Increased implementation complexity for job orchestration.
- Storage schema expands to include `jobs` and `job_steps`.
- Worker becomes a long-running process (`ch serve`) that must be managed.

### Neutral / Considerations

- Performance impact minimal since SQLite WAL supports fast writes.
- Testing matrix grows (pending → running → completed → retry → failed).
- Users may expect job introspection; CLI must expose `ch jobs list`, etc.

---

## Implementation Notes

- Add tables:
  - `jobs (id, type, payload, status, retries, created_at, updated_at, last_error)`
  - `job_steps (id, job_id, step, status, started_at, finished_at, logs)`

- Modify `ch analyze`:
  - Detect input type
  - Normalize input
  - Write job to DB
  - Return Job ID
  - Optionally support `--wait` to block until completed

- Implement `ch serve` (worker):
  - WAL-safe SQLite single-writer mode
  - Poll pending jobs
  - Execute pipelines
  - Write bookmark only on *completion*
  - Update statuses and retries atomically

- Future-proof:
  - Support priority queues
  - Support plugin-registered job types
  - Trigger chain jobs (e.g., fetch GitHub metadata after ingesting repo URL)

- Testing:
  - Crash mid-job → job remains pending or marked stale
  - Worker resumes automatically
  - Steps executed exactly once (idempotent)

---

## References

- ADR-003: Separation of Write Path vs Read Path
- ADR-004: Step-Based Pipeline Architecture
- ADR-012: Plugin Extensibility Across All Layers
- Microsoft Kernel Memory's ingestion approach (ContentStorageService + OperationRecord)
- Martin Fowler: Transactional Outbox Pattern
- SQLite WAL documentation
- Resilient job processing patterns (Sidekiq, Celery, Faktory for conceptual inspiration)

---