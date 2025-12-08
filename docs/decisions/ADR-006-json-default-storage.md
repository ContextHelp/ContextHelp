# ADR-006 – JSON Filesystem as the Default Storage Backend with Pluggable Options

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp requires a local-first, offline-capable storage layer that is reliable, cross-platform, and easy to distribute. The engine ingests diverse content types (text, URLs, images, audio transcripts, video transcripts, metadata) and generates structured bookmarks enriched with tags, summaries, sections, decisions, and AI-derived fields.

Key constraints include:

- **Local-first architecture:** Users must be able to run ContextHelp entirely offline.
- **Transparency & Introspection:** In early development stages, developers and power users need to manually inspect, edit, or version-control their data without specialized DB tools.
- **Portability:** The CLI and server must run on macOS, Linux, and Windows with zero external runtime dependencies.
- **Concurrency model:** Pipelines write via a job system; ingestion requires safe access, though early single-user scale is low.
- **Extensibility:** Users may eventually need SQLite or Postgres for high volume, but the initial need is low-friction onboarding.
- **Simplicity for distribution:** Binaries should ideally be pure Go (no CGO) for maximum portability during the bootstrapping phase.
- **Ecosystem support:** Registries are not yet fully implemented, reducing the immediate need for complex relational joins.

Several storage technologies were evaluated: JSON files, SQLite, Postgres, DuckDB, and KV stores.

This ADR describes the decision to use **JSON Filesystem** storage as the default backend to maximize simplicity and debuggability, while keeping the storage layer fully pluggable to support SQLite or Postgres later.

Subsystems impacted:
- Ingestion pipeline
- Job system / transactional outbox
- Bookmark storage
- Query execution
- Configuration loader

Goals optimized:
- Human-readability
- Debuggability
- Zero-dependency distribution (Pure Go)
- Simple onboarding
- Clear separation of storage interfaces

---

## Decision

**We will use a JSON Filesystem-based engine as the default storage backend for ContextHelp to prioritize transparency and ease of development, while exposing a pluggable storage interface that enables SQLite (and others) as optional adapters for production-scale use.**

---

## Rationale

### Why JSON as the default

- **Human-readable:** Data is stored as plain text. Users can debug, edit, or `git commit` their knowledge graph easily.
- **Zero dependencies:** Pure Go implementation requires no CGO, making cross-compilation trivial for all platforms.
- **Zero configuration:** No database initialization or connection strings required. It just writes files.
- **Bootstrapping speed:** Allows rapid iteration on the data schema before locking it down with SQL migrations.
- **Sufficient for MVP:** Since complex Registry relationships do not yet exist, complex relational queries are not yet required.

### Why not SQLite as the default (yet)

- **CGO Overhead:** SQLite often requires CGO (or modernc transpilation), which adds friction to the build pipeline.
- **Opaque Data:** Binary files require specific tools to inspect. Debugging schema issues is slower.
- **Complexity:** Relational schemas and migrations are overkill for the initial single-user prototype phase.

However, SQLite is maintained as a **first-class optional backend** for users who need FTS5, higher concurrency, or larger datasets.

### Why not Postgres as the default

- Requires server setup
- Breaks offline-first philosophy
- Overkill for single-user use cases

### Why pluggable backends

- Allows the system to scale from a simple text-file lists to enterprise-grade databases without code changes.
- Ensures the core logic remains decoupled from the physical storage medium.
- Future-proofs the architecture for when Registries introduce complex graph data.

### Alignment with decentralization

JSON files are the ultimate portable format. They can be synced via Dropbox, Git, or IPFS easily, aligning with the goal of user sovereignty.

---

## Consequences

### Positive

- **Maximum Debuggability:** Inspecting the job queue or bookmark store is as simple as `cat` or opening a text editor.
- **Trivial Distribution:** No shared libraries or C compilers needed to build the default binary.
- **Schema Flexibility:** Adding fields during early development doesn't require writing SQL migration scripts.
- **Git Friendly:** Users can version control their own data directory.

### Negative

- **Performance Limits:** Linear scans for search will degrade once bookmarks exceed ~10,000 items.
- **Weak Concurrency:** JSON files generally require file locking, effectively serializing writes. (Mitigated by the single-worker Job architecture).
- **No Native FTS:** Search is limited to substring matching unless an in-memory index is built on startup.
- **Durability Risks:** Writes are atomic file swaps, but lack the rigorous WAL journaling of SQLite.

### Neutral / Considerations

- The storage interface must be strict so that switching to SQLite later is purely a config change.
- Migrations from JSON to SQLite will eventually be needed as users scale up.

---

## Implementation Notes

- Introduce a `Storage` interface including:
  - `BookmarkStore`
  - `JobStore`
- JSON implementation lives in `storage/jsonfs/`.
  - Structure: `~/.contexthelp/data/bookmarks/` (one file per ID) and `~/.contexthelp/data/jobs/`.
  - This avoids loading a massive monolithic JSON file into RAM.
- Use file locking (`flock`) to ensure the Job Worker has exclusive write access.
- The configuration system supports:

  ```yaml
  storage:
    type: json
    path: "~/.contexthelp/data"
  ```

- Alternative backends (like SQLite) are registered via polymorphic config:

  ```yaml
  storage:
    type: sqlite
    path: "~/.contexthelp/ch.db"
  ```

- Test suite uses the JSON backend by default for simplicity, but CI should run tests against SQLite adapter to ensure contract compliance.

---

## References

- ADR-003 – Separation of write vs read paths
- ADR-007 – Transactional outbox for ingestion
- Go `encoding/json` documentation
- Flat-file CMS architecture patterns
- Discussions on bootstrapping complexity vs. long-term scalability