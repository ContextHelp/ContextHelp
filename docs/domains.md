# Domain Index: dPKMS + `ctxt`

This document provides a comprehensive index of all domains across both the **dPKMS** (substrate) and **`ctxt`** (brain) packages, organized by responsibility and cross-cutting concerns.

**Last Updated:** 2026-01-26

---

## Package Overview

- **dPKMS** — Decentralized knowledge substrate providing mechanical guarantees (storage, jobs, security, federation)
- **`ctxt`** — Agentic context brain providing meaningful behaviors (capture, enrichment, surfacing, composition)

Together they provide **context-as-a-service** for humans and AI agents.

---

## Domain Classification

### dPKMS Domains (Substrate/Mechanics)
Domains focused on **correctness, durability, and verifiable execution**

### `ctxt` Domains (Brain/Meaning)
Domains focused on **value, intelligence, and actionable outputs**

### Cross-Cutting Domains
Domains that span both packages with shared contracts

---

# dPKMS Domains

## 1. Storage Domain
**Location:** `docs/dpkms/storage.md`

**Responsibilities:**
- Storage backend abstraction (SQLite, Postgres, pluggable)
- Schema management and migrations
- Knowledge object persistence
- Index management (metadata, FTS5, vector, graph adjacency)
- Database configuration and optimization (WAL mode, cache settings)
- Stable ID preservation across exports
- Reversible migration paths

**Key Interfaces:**
- `ObjectStore` — Knowledge objects
- `JobStore` — Job queue operations
- `GraphStore` — Entity and edge storage
- `EntityStore` — Canonical entity definitions
- `VectorStore` — Optional embedding storage

**Storage Backends:**
- SQLite (default, local-first)
- PostgreSQL (optional, multi-user)
- Pluggable community backends

---

## 2. Jobs & Ingestion Domain
**Location:** `docs/dpkms/jobs-and-ingestion.md`, `docs/dpkms/queue.md`

**Responsibilities:**
- Transactional job queue (outbox pattern)
- Job lifecycle management (Pending → Running → Completed/Failed/Aborted)
- Worker coordination and concurrency
- Retry logic with exponential backoff
- Job state persistence and recovery
- Background processing without blocking CLI/API
- Crash-safe ingestion guarantees
- Job steps and progress tracking
- Priority-based queues (high, normal, low)

**Job Types:**
- `pipeline.run` — Execute enrichment pipeline
- `github.metadata.fetch` — External metadata retrieval
- `registry.sync` — Registry synchronization
- Custom plugin-defined job types

**Queue Backends:**
- In-memory (volatile, dev/testing)
- SQLite-backed (persistent, recommended)
- External providers (Redis, NATS, Postgres)

---

## 3. Pipeline Runtime Domain
**Location:** `docs/dpkms/jobs-and-ingestion.md` (runtime layer), `docs/design.md`

**Responsibilities:**
- Generic pipeline execution substrate
- Step isolation and typed I/O
- Capability enforcement (scoped access to storage, graph, crypto, registries)
- Caching hooks for idempotency
- Deterministic replay for debugging
- Structured logging and audit trails
- Permission and capability enforcement
- Panic isolation and recovery

**Guarantees:**
- Idempotent execution
- Deterministic (same inputs → same outputs)
- Safe to retry
- No side-effects except final write
- Traceable through `job_steps` table

**Pipeline Step Interface:**
```
Name()
Execute(ctx, input) → (output, error)
Retryable() bool
```

---

## 4. Query Engine Domain
**Location:** `docs/dpkms/query-language-spec.md`

**Responsibilities:**
- RSQL-based query language with extensions
- AST parsing and compilation
- Query plan generation and execution
- Hybrid retrieval (SQL + FTS + vector + graph)
- Query optimization
- Explainable match reasoning

**Query Operators:**
- Equality: `==`, `!=`
- Range: `<`, `>`, `>=`, `<=`
- IN/OUT: `=in=`, `=out=`
- Logical: `;` (AND), `,` (OR)
- Extended: `pipeline==`, `registry==`, `lang==`, `similar==`

**Query Plan Mapping:**
- Metadata → SQL
- FTS → SQLite FTS5 query
- Vectors → vector store search
- Registries → remote `/search` queries
- Graph → traversal operators
- Reranker → merge and deduplicate

---

## 5. Knowledge Graph Domain
**Location:** `docs/dpkms/knowledge-graph.md`

**Responsibilities:**
- Graph index (object ↔ entity, entity ↔ entity)
- Entity storage and canonical resolution
- Edge management and traversal
- Graph algorithms (pathfinding, clustering)
- Bidirectional relationship tracking
- Graph-aware retrieval and expansion

**Graph Components:**
- **Entities** — Canonical concepts with stable IDs
- **Edges** — Typed relationships (mentions, related, derives_from)
- **Nodes** — Knowledge objects and entities
- **Adjacency Indexes** — Fast traversal

**Edge Types:**
- `mentions` — Object → Entity
- `related` — Entity ↔ Entity
- `derives_from` — Object → Object

---

## 6. Mentions Domain
**Location:** `docs/dpkms/mentions.md`

**Responsibilities:**
- Mention extraction and parsing (`@entity.slug`)
- Canonical entity reference resolution
- Mention syntax validation
- Storage semantics (flat list, no duplicates)
- Lazy resolution (at query time, not extraction)
- Graph linking via mentions

**Mention Syntax:**
- Prefix: `@`
- Allowed: `a-z`, `0-9`, `.`, `_`, `-`
- Namespaced: `@domain.subdomain.entity`
- Examples: `@ui.best-practice`, `@stripe.api.checkout.v2`

**Parsing Rules:**
- Scan for `@`
- Capture until invalid character
- Normalize to lowercase
- Validate slug
- Store as literal string list

---

## 7. Ranking & Reranking Domain
**Location:** `docs/dpkms/ranking-and-reranking.md`

**Responsibilities:**
- Result merging across local and remote sources
- Reranking algorithms (RRF, weighted sum, softmax)
- Deduplication strategies (ID, URL, embedding similarity)
- Score normalization across incompatible sources
- Explainability metadata (`rank.explain`)
- Source trust weighting

**Reranking Algorithms:**
- **Reciprocal Rank Fusion (RRF)** — Position-based merging
- **Weighted Sum Ranking** — Score-based combination
- **SoftMax Normalization** — Probability distribution
- **Plugin-provided methods** — Custom ranking logic

**Deduplication:**
- Exact ID match
- URL normalization
- Semantic fingerprint (embedding cosine)

---

## 8. Registry & Federation Domain
**Location:** `docs/dpkms/registries.md`, `docs/dpkms/registry-protocol.md`, `docs/dpkms/registry-syncing-and-retrieval.md`, `docs/dpkms/decentralization.md`

**Responsibilities:**
- Registry protocol implementation
- Registry connectors (local, remote, federated)
- Scatter-gather retrieval across sources
- Registry syncing (snapshot, delta)
- Conflict resolution and namespace management
- Registry authentication (API key, OAuth device flow)
- Trust models and credibility scoring

**Registry Types:**
- Taxonomy registry
- Tag registry
- Bookmark registry
- Multi-purpose registry

**Registry Endpoints:**
- `/tags` — Tag vocabulary
- `/taxonomy` — Hierarchical classification
- `/vocabulary` — Shared terminology
- `/search` — Federated search
- `/metadata` — Registry info
- `/sync/snapshot` — Full sync
- `/sync/delta` — Incremental sync
- `/assets` — Media files
- `/embeddings` — Vector data

**Conflict Resolution:**
- Namespaced labels
- Canonical registry preference
- Alias resolution rules
- User override support

---

## 9. Embeddings Domain
**Location:** `docs/dpkms/embeddings.md`

**Responsibilities:**
- Embedding lifecycle management
- Vector storage and indexing
- Semantic similarity search
- Staleness detection and refresh policies
- Embedding provider abstraction
- Incremental embedding updates

**Staleness Policies:**
- Content change detection
- Time-based refresh (configurable TTL)
- Model version updates
- Manual refresh triggers

**Embedding Providers:**
- OpenAI (text-embedding-3-large)
- Custom provider plugins
- Local embedding models

---

## 10. Caching Domain
**Location:** `docs/dpkms/caching.md`

**Responsibilities:**
- Query result caching
- Pipeline step caching
- Embedding caching
- Cache invalidation policies
- Cache storage backends
- TTL and eviction strategies

**Cache Types:**
- Query cache — Search results
- Step cache — Pipeline outputs
- Embedding cache — Vector representations
- Metadata cache — Registry data

---

## 11. Security Domain
**Location:** `docs/dpkms/security.md`, `docs/security/`

**Responsibilities:**
- Encryption at rest and in transit
- Key management (pluggable providers)
- Authentication mechanisms
- Authorization and capability scoping
- Audit logging
- Cryptographic signing and verification
- Tamper-evident history
- Privacy-preserving search

**Security Features:**
- Optional encryption for storage
- Self-authenticating bundles
- Scoped permissions for workspaces
- Token-based access control
- Capability-scoped plugin execution

---

## 12. Privacy Domain
**Location:** `docs/dpkms/privacy.md`

**Responsibilities:**
- Data isolation and workspace boundaries
- Privacy-preserving search modes
- Sharing controls and handshakes
- Data portability guarantees
- Export/import with integrity
- Controlled sharing workflows

**Privacy Guarantees:**
- Private-by-default workspaces
- Intentional publishing
- Scoped subscriptions
- Reversible sharing
- No centralized authority requirement

---

## 13. Decentralization Domain
**Location:** `docs/dpkms/decentralization.md`

**Responsibilities:**
- Federated architecture design
- Registry distribution protocols
- Conflict resolution strategies
- Trust models (reputation, signatures)
- Namespace management
- Portable trust without centralized authority

---

## 14. Schema Management Domain
**Location:** `docs/dpkms/storage.md`, `docs/dpkms/schema-entity.md`, `docs/dpkms/schema-registry.md`

**Responsibilities:**
- Entity schema definitions
- Registry schema definitions
- Schema versioning and migrations
- Backward compatibility guarantees
- Stable ID preservation

**Entity Schema:**
```
id (canonical slug)
title, description
aliases (JSON array)
translations (JSON map)
metadata (JSON)
namespace, version
registry_source
```

**Registry Schema:**
```
name
url
type (taxonomy, tags, bookmarks)
auth model
sync endpoints
metadata
```

---

## 15. Testing Domain (dPKMS)
**Location:** `docs/dpkms/testing.md`

**Responsibilities:**
- Unit testing strategies (mocked AI & storage)
- Pipeline tests
- Job retry tests
- FTS integration tests
- Registry mocks
- Query parser tests
- Reranker tests
- E2E tests (analyze → job → worker → bookmark)

---

# Total Domains: 40

**15 dPKMS + 17 ctxt + 8 Cross-Cutting**

For the complete list including all ctxt and cross-cutting domains, see the full domains.md file.
