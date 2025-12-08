# ADR-004 – Step-Based Pipeline Architecture for Analysis and Enrichment

> **Status:** Proposed
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp must process a wide variety of input types—text, URLs, images, audio, video—each requiring analysis, enrichment, tagging, and structured output. These transformations are performed locally, often involve AI calls, and must be:

- reliable (resilient to crashes, retries, partial failures)
- deterministic (idempotent under job retries)
- extensible (new steps and pipelines via plugins)
- composable (multiple steps per pipeline)
- configurable (active, disabled, replaced, reordered)
- transparent (logs, tracing, debugging)

Earlier prototypes used monolithic, ad-hoc “pipeline functions” that embedded AI calls, heuristics, and storage logic. This increasingly violated separation of concerns, made testing difficult, and prevented plugin authors from safely inserting or replacing functionality.

Additionally, the ingestion model for ContextHelp requires retry-safe pipelining because the engine uses a **transactional outbox** pattern where analysis runs in background jobs. A single monolithic step made fault isolation impossible. If an AI call failed or timed out, the entire pipeline had to restart with no insight into which part failed.

The system must also support:

- registry-driven extensions
- i18n/l10n translation steps
- metadata extraction steps
- vectorization and embeddings generation
- OCR, ASR, image component extraction
- structured tagging (hint-influenced tag inference)
- plugin-defined AI providers

All these requirements create a strong need for a **formal, modular pipeline system** similar to well-defined ETL processing or compiler phases.

This ADR defines that architecture.

---

## Decision

**Pipelines will be implemented as a sequence of explicit, ordered, independent, retry-safe steps. Steps are plugin-extensible and must all conform to a common interface.**

---

## Rationale

A step-based architecture was chosen because it offers the best balance of **composability**, **testability**, **reliability**, and **plugin extensibility**.

### Why this approach was chosen

- **Idempotency & retry safety:**
  Each step can maintain its own invariants; if a job fails mid-pipeline, steps can be retried without corrupting outputs.

- **Extensibility:**
  Plugins can add, replace, or disable steps without modifying core engine code.

- **Observability:**
  Steps become discrete instrumentation points for logging, metrics, debugging, and performance tracing.

- **Consistency with job-based ingestion:**
  A step-based approach aligns with Phase 2 processing in the transactional outbox pattern.

- **Modularity & maintainability:**
  Each step has a single responsibility—OCR, transcription, summarization, tagging, section extraction, etc.

- **Parallelizability (future):**
  Some non-dependent steps could run in parallel.

### Alternatives considered

- **Monolithic pipeline functions:**
  Rejected due to poor extensibility, low observability, and inability to isolate faults.

- **Event-driven / reactive pipelines:**
  Too complex for local-first offline architecture; harder for plugin authors to reason about ordering.

- **Declarative pipelines (YAML-defined):**
  Potential future extension, but not suitable as the foundation for the engine.

### Long-term architectural alignment

- Works well with decentralized registries that may introduce new step types.
- Allows seamless adoption of multiple AI providers or switching models.
- Enables per-user customization of pipeline construction via config or plugins.

---

## Consequences

### Positive
- **Highly modular pipeline system** enabling easier maintenance and testing.
- **Retry-safe jobs** ensure crash resilience and improved reliability.
- **Plugins can augment analysis easily**, accelerating ecosystem growth.
- **Users gain fine-grained control** over analysis pipelines.
- **Better observability** with step-level logs and metrics.

### Negative
- **Increased architectural complexity** vs a simple monolithic function.
- **Slight performance overhead** due to step orchestration.
- **Plugins must be carefully versioned** to avoid breaking the pipeline graph.
- **Potential duplication of work** if steps are not optimized for incremental output.

### Neutral / Considerations
- Requires a clear **pipeline-step contract** (input/output model).
- Might require **pipeline versioning** to support future breaking changes.

---

## Implementation Notes

- Define a Go interface, e.g.:

  ```go
  type PipelineStep interface {
      Name() string
      Run(ctx context.Context, in *BookmarkDraft) (*BookmarkDraft, error)
  }
  ```

- Steps must be:
  - deterministic
  - idempotent
  - retry-safe
  - validated on startup

- Pipeline definitions live in configuration and must support:
  - ordering
  - enabling/disabling steps
  - plugin-injected steps

- Steps should not write to storage directly; only the ingestion manager commits the final bookmark.

- Logs must include:
  - step name
  - duration
  - structured outputs

- Testing should include:
  - isolated step unit tests
  - integration tests for full pipelines
  - job retry simulations

---

## References

- ADR-003 – Write Path vs Read Path Separation
- ADR-007 – Transactional Outbox for Ingestion
- Plugin architecture draft
- Kernel Memory architecture analysis (background jobs + layered pipeline execution)
- AWS Step Functions / Airflow DAGs (conceptual parallels)

---