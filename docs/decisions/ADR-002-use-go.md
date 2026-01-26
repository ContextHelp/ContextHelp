# ADR-002 – Use Go as the Primary Implementation Language for ContextHelp

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp is a local-first, decentralized knowledge engine designed to run on user machines with minimal dependencies, while supporting a rich plugin ecosystem, background job execution, pipelines, registries, and AI-powered analysis.
The system must operate reliably in several modes:

- **CLI** (`ch`)
- **Local server** (REST + gRPC API)
- **Background worker** (processing ingestion jobs)
- **Plugin host** (pipeline steps, AI providers, registry adapters)

To fulfill these roles, the engine must:

- compile to a **single, static, fast, portable binary**
- have **strong concurrency** support for workers and background pipelines
- support a **robust plugin mechanism** without dynamic language pitfalls
- provide **excellent performance**, especially for local users and batch jobs
- run consistently on macOS, Windows, Linux without extra dependencies
- support **WAL-friendly local database access** (SQLite or other embedded storage)
- avoid the deployment complexity of runtimes like Node.js, Python, or JVM

Given the architecture’s direction—transactional outbox ingestion, registry protocol integrations, multi-source retrieval, reranking, structured storage, plugin extensibility—the choice of implementation language determines:

- runtime performance
- plugin model feasibility
- developer experience
- deployability
- reliability under load
- long-term maintenance cost

Multiple languages were considered, including:

- Node.js (original prototype)
- Rust
- Python
- C# / .NET
- Go

Each has strengths, but not all align with the ContextHelp architecture.

This ADR formalizes the decision to standardize on **Go (Golang)** as the implementation language for the ContextHelp engine.

---

## Decision

**ContextHelp will be implemented in Go as the primary language across the CLI, ingest worker, local server, registry adapters, pipelines, plugin system, and storage backend interfaces.**

---

## Rationale

### Why Go?

#### 1. **Static, Portable Binaries (No Runtime Dependencies)**
Go produces single-file executables with no required VM or runtime installation.
This perfectly aligns with the local-first philosophy.

#### 2. **Excellent Concurrency Model**
Goroutines + channels provide:
- lightweight workers
- fast background ingestion
- parallel registry queries
- asynchronous I/O

This strongly supports the ingestion engine + reranking system.

#### 3. **Stable Cross-Platform Story**
Cross-compiling for macOS, Linux, and Windows is trivial.
Users do not need Node/Python installed.

#### 4. **Plugin-Friendly Architecture**
Go supports:
- interfaces as extension points
- build-time or runtime plugin systems
- isolated modules for pipelines, storage, registry clients

Go’s strong type system and lightweight interfaces simplify plugin loading.

#### 5. **SQLite and Embedded DB Compatibility**
Go has mature SQLite bindings and tools (sqlc, sqlx).
No heavy ORM is required, aligning with ADR-006 and ADR-003.

#### 6. **Performance + Memory Safety**
While not as low-level as Rust, Go provides:
- safe memory model
- near-native performance
- much lower development friction

For an MVP and long-term maintainability, this balance is ideal.

#### 7. **Vibrant Ecosystem for CLI, Server, AI**
Go has strong libraries for:
- CLI frameworks
- HTTP/gRPC servers
- AI service clients
- Embedding/vector DB integrations

---

### Why Not the Alternatives?

#### Node.js
- Requires runtime installation
- Weak concurrency for CPU-bound tasks
- Plugin system fragile
- Memory footprint high

#### Rust
- Exceptional performance but slower development
- Poor plugin runtime story
- Steeper learning curve for contributors

#### Python
- Slow execution
- Heavy dependency graph
- Poor concurrency model

#### C# / .NET
- Large runtime
- Cross-platform less portable
- Distribution heavier than desirable

Go provides the best blend of **DX**, **runtime characteristics**, **tooling**, and **deployability**.

---

## Consequences

### Positive
- Single binary distribution simplifies installation
- Strong concurrency enables fast ingestion and multi-source retrieval
- Plugins can integrate safely with stable interface boundaries
- High performance for local-first workloads
- Cleaner cross-platform support
- Reduced maintenance overhead compared to Node/Python

### Negative
- Runtime plugin support in Go can be restrictive (requires clear boundaries or build-time plugin strategy)
- Go generics are improving but still limited
- Developers must be comfortable with static typing
- Interfacing with some AI libraries may require wrappers

### Neutral / Considerations
- Some advanced optimizations (SIMD, custom memory pools) might require CGO, which introduces complexity
- Go lacks full-featured dynamic runtime introspection compared to languages like Python

---

## Implementation Notes

- Create `cmd/ch` for CLI, `internal/engine` for ingestion & pipelines, `internal/server` for REST/gRPC.
- Introduce interfaces:
  - `PipelineStep`, `Pipeline`,
  - `RegistryClient`,
  - `StorageEngine`,
  - `EmbeddingClient`, `LLMClient`.
- Use `sqlc` or `sqlx` for SQLite/Postgres drivers (avoid heavy ORMs).
- Support plugin loading through:
  - Go plugin system (optional, build-tag constrained)
  - or RPC/sidecar model for maximum safety
- Ensure cross-compilation in CI workflows.
- Enforce static binary builds (`CGO_ENABLED=0` where possible).

---

## References

- ADR-003 – Separation of Write and Read Paths
- ADR-006 – Storage backend selection & pluggability
- ADR-012 – Plugin-first architecture
- Kernel Memory architecture research
- SQLite WAL best practices
- Go official docs: https://go.dev

---