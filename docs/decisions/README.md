# Architectural Decision Records (ADRs)

This directory contains the **core architectural decisions** that define how ContextHelp is designed, implemented, extended, and maintained.
Each file documents a high-impact, long-lived decision that shapes the system’s architecture.
ADRs exist to provide clarity, preserve rationale, and guide future contributors as the project evolves.

Below is an overview of all recorded ADRs and their purpose.

---

## Index of Decisions

### **ADR-001 – Local-First and Decentralized**
ContextHelp runs fully on the user's machine, functioning offline by default while optionally connecting to remote registries.
This philosophy ensures privacy, control, and resilience.

### **ADR-002 – Use Go**
Go is the primary implementation language for the engine, CLI, worker, pipelines, and plugin system.
Selected for concurrency primitives, static binaries, plugin capabilities, and cross-platform support.

### **ADR-003 – Separate Read/Write Paths**
Ingestion (write path) is completely separated from retrieval (read path).
Prevents pipelines from interfering with search and ensures predictable performance.

### **ADR-004 – Step-Based Pipeline Architecture**
Pipelines are modular sequences of retry-safe, independently testable steps.
This enables plugin-driven pipeline registration and multi-AI workflows.

### **ADR-005 – Decorator Pattern for AI Providers**
AI clients (LLMs, embeddings, OCR) are extended via decorators rather than internal logic.
Caching, logging, and tracing are layered without altering core clients.

### **ADR-006 – SQLite as Default Storage**
SQLite with WAL mode is the default storage backend.
The system remains pluggable: Postgres, JSONFS, KV stores, or external engines may replace SQLite.

### **ADR-007 – Transactional Outbox for Ingestion**
Ingestion uses a durable job queue (the “transactional outbox”).
`ch analyze` enqueues a job → worker executes pipeline → job state updated → crash recovery guaranteed.

### **ADR-008 – Remote Knowledge Registry Protocol**
Remote registries follow a defined protocol for:
- taxonomy
- tags
- metadata
- weights
- translations (optional)
- custom extensions

Registries act as authoritative knowledge sources.

### **ADR-009 – Multi-Source Retrieval (Optional Sync)**
Search queries fan out to:
- local database
- remote registries
- plugin-defined sources

Optional per-registry syncing enables local indexing when desired.

### **ADR-010 – Use Extended RSQL Query Language**
ContextHelp uses **RSQL as the base query language**, extended with:
- semantic operators
- pipeline filters
- language filters
- registry-scoped fields

All queries parse into an AST for correctness and safety.

### **ADR-011 – Multi-Source Reranker**
Search results from all sources are merged via a dedicated reranker
(e.g., Reciprocal Rank Fusion, weighted scoring).
Ensures consistent, meaningful ranking across heterogeneous sources.

### **ADR-012 – Plugins Extend Any Layer**
Plugins are first-class citizens and may extend:
- pipelines
- registries
- storage engines
- AI providers
- CLI commands
- query operators
- background observers

This is the foundation for the ecosystem.

---

## Purpose of This Directory

This directory contains:

- **Persistent historical decisions**
- **Context and justification** for each major choice
- Guidance for contributors evaluating new proposals
- A stable reference for architecture, design, and extensibility boundaries

Only decisions that materially affect the project’s trajectory are captured as ADRs.

---

## How to Use ADRs

### When adding new features or making architectural changes:
- Check whether an existing ADR already dictates the correct approach.
- If not, consider writing a new ADR following the `TEMPLATE.md` file.
- ADRs should be:
  - concise
  - rationale-rich
  - forward-looking
  - easy to read

### When reviewing contributions:
ADRs provide the criteria used to evaluate whether a PR adheres to the project’s architectural intent.

---

## Adding a New ADR

Use the template:

```
TEMPLATE.md
```

Follow the structure:
1. Title
2. Status
3. Context
4. Decision
5. Consequences
6. Alternatives considered

Propose a new ADR only when the decision is:
- foundational
- difficult to reverse
- cross-cutting
- high-impact

Small implementation details typically belong in design docs, not ADRs.

---

## Summary

This directory contains the **canonical architectural backbone** of ContextHelp.
Together, these ADRs encode the philosophy, constraints, and technical direction of the system, ensuring stability and consistent decision-making as the ecosystem grows.

If you are contributing to ContextHelp, this is one of the most important directories to understand.