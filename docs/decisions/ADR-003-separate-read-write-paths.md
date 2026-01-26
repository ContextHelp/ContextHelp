# ADR-003 – Separate Write Path (Ingestion) from Read Path (Retrieval)

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** N/A
> **Superseded by:** N/A

---

## Context

ContextHelp ingests many different types of content—text, URLs, images, audio, video, clipboard states—and processes them through AI-backed pipelines that may be slow, compute-heavy, or fail unpredictably (LLM timeouts, OCR failures, out-of-memory conditions, user interruptions, etc.).

In the early prototype, ingestion and retrieval were part of a single execution flow: `ch analyze` both *processed* content and *wrote* results directly into storage. This tightly coupled design created several systemic issues:

- **Pipeline fragility:** If the user killed the terminal, or the AI provider timed out, the work was lost—no retry, no state recovery.
- **Poor UX:** Users expect ingestion to be instant, with heavy processing happening asynchronously.
- **Non-idempotent writes:** Pipelines sometimes produced partial output; retriggering an ingestion would create duplicate or inconsistent records.
- **Inability to batch or schedule workloads:** Long-running steps (e.g., video transcription) blocked the normal CLI flow.
- **Complex retrieval semantics:** Retrieval couldn’t assume the database was in a consistent state while pipelines were mid-execution.
- **Future decentralization constraints:** Registries, background metadata fetchers, and embeddings jobs all require async workflows with reliable state.

Industry systems handling knowledge ingestion—including Microsoft’s Kernel Memory—use a **transactional outbox pattern** to ensure that ingestion jobs are persisted *before* doing any heavy work, enabling safe retries and resilience.

This ADR defines the architectural separation between the **Write Path** (ingestion → enqueue job → return job ID) and the **Read Path** (search/retrieval over consistent bookmark data only).

This decision impacts:
- CLI (`ctxt analyze`, `dpkms serve`)
- Pipelines
- Storage/migrations
- Worker/queue system
- Registries (eventually)
- Plugins that add ingestion steps or new pipeline types

The goal is to maximize reliability, reduce user-facing latency, and create a robust foundation for decentralization and long-term background automation.

---

## Decision

**ContextHelp will separate ingestion (Write Path) from retrieval (Read Path) using a transactional outbox job system. `ctxt analyze` will only enqueue a job; a worker process will execute pipelines and write bookmarks when the job completes.**

---

## Rationale

### Why this approach was chosen

1. **Crash-safety is mandatory.**
   Pipelines can take seconds or minutes and frequently depend on external APIs. A crash or interrupt must *not* destroy work. A durable job queue ensures retryability and state recovery.

2. **Fast UX for users.**
   Ingestion must feel instantaneous—especially for hotkeys, Raycast triggers, and high-frequency capture workflows. Enqueuing a job is always fast.

3. **Consistency in the bookmark store.**
   Retrieval must operate only on fully processed, valid bookmarks; no half-written or partially processed entries.

4. **Pipeline robustness and introspection.**
   Jobs allow tracing, debugging, timing metrics, retries, and failure reporting without polluting bookmark data.

5. **Enables distributed and plugin-based execution.**
   Plugins may register new pipeline steps, AI providers, or background workers. A job system provides a universal execution layer.

6. **Future scalability and decentralization.**
   Registries, sync operations, and metadata-fetch jobs fit naturally into the same job engine.

### Alternatives considered

#### **A. Synchronous ingestion inside CLI**
Rejected because it:
- blocks user workflows
- loses work on crash
- complicates pipelines
- prevents background automation

#### **B. In-memory queue + best-effort writes**
Rejected:
- no durability guarantees
- cannot support retries
- unsafe for long-running pipelines

#### **C. External queue system (Redis, NATS, SQS)**
Rejected for MVP:
- breaks local-first constraint
- adds operational overhead
- unnecessary before multi-node scaling

#### **D. Direct writes with partial rollback**
Rejected:
- too complex
- pipeline output is not atomic
- rollback logic becomes brittle

### Long-term implications
- ContextHelp supports high-frequency ingestion workflows safely.
- Architecture aligns with decentralized, plugin-friendly, registry-driven future.
- Worker services (e.g., `ch serve`) become first-class parts of the ecosystem.

---

## Consequences

### Positive
- **Crash-safe ingestion** with full recovery and retries.
- **Instant feedback** for users running `ctxt analyze`.
- **Consistent bookmark database**—no half-processed data.
- **Extensible job engine** enabling:
  - background metadata jobs
  - embeddings updates
  - registry sync tasks
  - plugin-triggered workflows
- **Cleaner architecture:** read path is purely retrieval; write path is controlled and deterministic.

### Negative
- Requires a **job runner** (`dpkms serve`) to be running for ingestion to complete.
- Adds **schema complexity** (`jobs`, `job_steps` tables).
- Requires **rerun logic and idempotency** in pipelines.
- CLI may need a `--wait` flag to support synchronous workflows.
- Contributors must understand two execution modes (enqueue vs execute).

### Neutral / Considerations
- Multiple worker support may be needed later.
- Migration from synchronous behavior requires community documentation.
- Plugins must integrate with the job system rather than bypassing it.

---

## Implementation Notes

- Create two new tables:
  - `jobs` (id, status, type, created_at, started_at, completed_at, retries, payload JSON)
  - `job_steps` (optional: per-step traces, logs, error details)

- `ctxt analyze`:
  - parse input → infer pipeline → serialize payload → insert into `jobs` as `Pending` → return job ID.

- `dpkms serve` (or embedded worker):
  - poll for `Pending` jobs → mark as `Running` → execute pipeline → write bookmark → mark `Completed` or `Failed`.

- Pipelines must be **idempotent** and can use `job_steps` for intermediate state tracking.

- Retrieval path (`ctxt list`, REST/gRPC search) only queries the `bookmarks` table.

- Test cases:
  - simulate crash after job creation
  - simulate crash mid-step
  - retry logic
  - duplicate ingestion protection

---

## References

- ADR-004 (Pipeline architecture)
- ADR-006 (SQLite as default storage)
- Microsoft Kernel Memory design patterns
- https://martinfowler.com/articles/patterns-of-distributed-systems/transactional-outbox.html
- Internal discussions on ingestion reliability (2025-09–2025-10)

---