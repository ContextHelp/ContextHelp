# ADR-014 – Two-Package Architecture: dPKMS (Substrate) and ctxt (Brain)

> **Status:** Accepted
> **Date:** 2026-01-25
> **Author:** @jadb
> **Applies to:** ContextHelp (project-level)
> **Supersedes:** N/A
> **Superseded by:** N/A

---

## Context

ContextHelp began as a monolithic system where ingestion, storage, retrieval, and user-facing behavior were tightly coupled. As the project evolved toward a broader "ContextHelp OS" vision with multiple interfaces (CLI, daemon, libraries, future GUI), several issues emerged:

- **Monolithic binary:** All functionality (storage, pipelines, queries, UI behavior) lived in a single binary, making it hard to evolve interfaces independently.
- **Unclear boundaries:** Contributors struggled to distinguish between infrastructure concerns (job execution, storage) and behavioral concerns (what to analyze, how to present results).
- **Multiple use cases:** Some scenarios required only the execution substrate (background daemon, library use), while others needed the full user-facing intelligence.
- **Future extensibility:** Adding new interfaces (TUI, GUI, mobile, web) would require duplicating logic or creating awkward dependency graphs.

The original design treated "ContextHelp" as a single entity, implicitly mixing concerns across layers. This made it difficult to:

- Test infrastructure independently of behavior
- Substitute different user interfaces without touching core execution
- Scale or optimize one layer without affecting the other
- Document clear contracts between components

As the architecture matured and features like multilingual support, mentions, knowledge graphs, and plugin ecosystems were added, the need for explicit separation became critical.

---

## Decision

**ContextHelp will adopt a two-package architecture separating concerns into:**

- **dPKMS (substrate):** A foundational package that owns the mechanics of running work safely—transactional job queue, pipeline runtime, storage, indexing, and persistence guarantees.
- **ctxt (brain):** A higher-level package that owns intent, meaning, and composition—deciding what work to run, defining pipelines, and providing user-facing capture/retrieval behavior.

Both packages are Go projects with their own binaries (`dpkms` and `ctxt`), but they share architectural principles and follow the same ADRs where applicable. The `ctxt` binary wraps and uses `dpkms` capabilities; `dpkms` can also be used independently as a library or daemon.

See `docs/dpkms-or-ctxt.md` for the detailed boundary specification.

---

## Rationale

### Why separate dPKMS and ctxt?

1. **Clear separation of concerns**
   - dPKMS: *Can this work run safely?* (infrastructure)
   - ctxt: *Should we run this work?* (behavior)

2. **Independent evolution**
   - dPKMS can optimize storage, concurrency, and reliability without changing user behavior
   - ctxt can evolve pipelines, AI models, and presentation without touching infrastructure

3. **Multiple consumption modes**
   - Background daemon: Use `dpkms serve` directly
   - User CLI: Use `ctxt analyze`, `ctxt list`, which delegate to dPKMS
   - Library: Import `dpkms` package for custom integrations
   - Future GUIs/TUIs: Can use either or both

4. **Testing and maintainability**
   - Infrastructure tests (storage, jobs) isolated from behavior tests (pipelines, AI)
   - Clear contracts between layers make debugging easier
   - Plugins can target either layer appropriately

5. **Plugin ecosystem clarity**
   - Storage plugins integrate with dPKMS
   - Pipeline/AI plugins integrate with ctxt
   - Boundaries prevent plugins from overstepping their intended scope

### Why not keep them merged?

- **Coupling:** Changes to storage would affect user behavior, and vice versa
- **Binary size:** Monolithic binary grows with every feature, even unused ones
- **Deployment complexity:** Different deployment scenarios (daemon vs CLI vs library) would all carry the same weight
- **Documentation:** Harder to explain what a plugin "extends" without clear boundaries

### Why not more than two packages?

Additional packages (e.g., separate "registry," "storage" packages) would introduce overhead:
- Cross-repository dependencies
- Versioning complexity
- Distribution challenges for a single-project codebase

The two-package split hits the sweet spot: minimal complexity with maximal clarity.

---

## Consequences

### Positive

- **Clear architectural boundaries** between infrastructure and behavior
- **Independent evolution** of each package
- **Flexible deployment:** Daemon-only, CLI-only, or combined usage
- **Better testing:** Infrastructure tests isolated from behavior tests
- **Clearer plugin contracts:** Storage vs pipeline vs AI plugins have distinct targets
- **Documentation precision:** Easier to explain which component handles what

### Negative

- **Two binaries instead of one:** Slightly more complex distribution
- **Inter-package API maintenance:** dPKMS and ctxt APIs must stay in sync
- **Learning curve:** Contributors must understand two-package relationship
- **Potential for drift:** Packages could diverge if not carefully coordinated

### Neutral / Considerations

- Shared ADRs: Many ADRs apply to both packages (marked with "Applies to: dPKMS, ctxt")
- Some ADRs are package-specific (e.g., ADR-006 applies only to dPKMS)
- Versioning: Both packages should version together in initial releases
- Integration tests: Must test inter-package behavior, not just intra-package

---

## Implementation Notes

### Binary Structure

- `cmd/dpkms/`: Daemon binary providing `dpkms serve` (job worker, API server)
- `cmd/ctxt/`: User-facing CLI providing `ctxt analyze`, `ctxt list`, `ctxt search`
- Both binaries import shared libraries from `internal/` and packages

### Package Boundaries

- **dPKMS provides:**
  - Job queue and worker implementation
  - Storage interface and backends (SQLite, etc.)
  - Pipeline runtime (step execution, isolation)
  - Registry protocol client infrastructure
  - Basic query execution (no behavioral logic)
  - Multilingual-safe storage and indexing

- **ctxt provides:**
  - Pipeline definitions (what steps, in what order)
  - AI provider selection and configuration
  - Job type definitions ("ingest:url", "enrich:audio")
  - Multi-source retrieval logic and reranking
  - User-facing CLI commands
  - Behavioral decisions (what to refresh, when to surface)

### Cross-Package APIs

- ctxt calls dPKMS via well-defined Go interfaces
- No direct database access from ctxt (go through dPKMS storage interface)
- Job enqueueing: ctxt → dPKMS jobs table
- Pipeline execution: dPKMS runtime → ctxt-registered steps

### Documentation

- `docs/dpkms-or-ctxt.md`: Comprehensive boundary specification
- ADR headers include "Applies to:" field to indicate package scope
- README and CONTRIBUTING guide explain two-package structure

---

## References

- **docs/dpkms-or-ctxt.md** – Detailed dPKMS vs ctxt boundary specification
- **ADR-003** – Separate Write/Read Paths (Applies to: dPKMS, ctxt)
- **ADR-004** – Step-Based Pipeline Architecture (Applies to: dPKMS, ctxt)
- **ADR-007** – Transactional Outbox for Ingestion (Applies to: dPKMS)
- **ADR-012** – Plugins Extend Any Layer (Applies to: dPKMS, ctxt)

---
