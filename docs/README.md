# ContextHelp Documentation Guide

Welcome to the **ContextHelp** documentation folder.
This directory contains all technical, architectural, conceptual, and specification documents that describe how the system works, how it is extended, and how developers interact with it.

Because ContextHelp is a **large, modular, decentralized context engine**, the docs are organized by domain.
Use this guide to understand what each document covers and where to go next.

---

## 📚 Core Architecture

### **architecture.md**

High-level system overview: ingestion, pipelines, registries, storage, APIs, agents, plugins, providers, caching decorators, search stack, semantic layers, mentions, entities, and decentralized worldview assembly.

### **design.md**

Global design orientation and philosophical principles behind ContextHelp.
Explains cross-cutting concerns, shaping forces, system boundaries, and long-term evolution.

### **leann-integration.md** (New)

Integration guide for [LEANN](https://github.com/yichuan-w/LEANN) — a local-first RAG system with 97% storage efficiency.
Covers storage backend configuration, hybrid storage strategies, MCP integration, and performance considerations.

### **decentralization.md**

How ContextHelp operates without central servers, how registries interact, trust boundaries, merging strategies, worldview layering, and local-first guarantees.

### **registries.md**

End-to-end documentation for registry interaction:
- taxonomy registries
- bookmark registries
- entity registries
- weights registries
- registry priorities, syncing, conflict resolution, namespace rules
- local-only vs remote registry mode

### **registry-protocol.md**

Formal protocol (wire-level and conceptual) for any service wishing to expose itself as a ContextHelp registry, including entity definitions, alias resolution, syncing, and metadata surfaces.

### **registry-syncing-and-retrieval.md**

Describes how registries are fetched, cached, versioned, merged, prioritized, and queried. Covers fallback behavior, multi-source ordering, and entity conflict resolution.

### **storage.md**

The storage layer:
- bookmark schema
- mentions storage
- entity index and backlinks
- storage backends (JSON, SQLite, Postgres, LEANN, plugin-defined)
- job tables and outbox pattern
- indexes, FTS, concurrency patterns
- update guarantees and invariants

### **jobs-and-ingestion.md**

Deep dive into ingestion internals:
- job creation (transactional outbox)
- job_steps table
- worker orchestration
- crash recovery
- retry logic
- idempotency

### **queue.md**

Queue characteristics:
- job state machine
- FIFO vs priority queues
- safe retries, backoff, leasing
- worker semantics and locking strategies

---

## 🧠 Knowledge & Semantics

### **hints.md**

User-provided semantic hints (#ui, #inspiration, #bad) and how they influence:
- pipelines
- tagging
- weighting
- interpretation
- worldview shaping
Clarifies the separation of hints, tags, and mentions.

### **tags.md**

AI-generated tags, their weights, polarity, and relation to taxonomies. Includes provenance and registry mapping, and distinctions from mentions and hints.

### **mentions.md**

Defines the canonical reference system:
- @syntax rules
- entity lookup
- aliasing
- stable identity
- storage rules
- backlinks
- unresolved vs resolved entities
- integration in pipelines across modalities

### **schema-bookmark.md**

Canonical bookmark schema:
- metadata
- sections
- decisions
- provenance
- embeddings (optional)
- translations
- hints, tags, mentions
- pipeline metadata
- plugin metadata namespace

### **schema-taxonomy.md**

Structure for taxonomy definition files:
- tag hierarchies
- categories
- controlled vocabulary rules
- translation fields
- differentiation between taxonomy labels and canonical entities

### **schema-entity.md**

Schema for canonical entities:
- id
- title
- description
- metadata
- aliases
- translations
- versioning rules
- registry provenance

### **schema-registry.md**

Defines how registries expose:
- taxonomies
- entity definitions
- tag expansions
- bookmarks
- sync metadata

### **schema-tag.md**

Tag schema specification:
- label
- polarity
- influences
- hierarchy
- weights
- provenance
- coexistence and boundaries with mentions

### **knowledge-graph.md**

Documentation of the knowledge graph:
- bookmark → entity edges
- entity → entity relations
- backlinks
- inferred clusters
- graph traversal rules
- usage patterns for agents and retrieval

---

## 🔄 Pipelines

### **pipelines.md**

Conceptual overview:
- inference system
- multimodal pipelines
- mention extraction and entity resolution
- step-based pipeline architecture
- plugin extensions
- execution lifecycle
- pipeline-level and global event hooks

### **pipelines-reference.md**

Details for each built-in pipeline:
- text.short, text.long
- url.article, url.repo, url.generic
- image.landing, image.ocr, image.component
- audio, video
- I/O schemas
- mention extraction per modality

---

## 🔍 Search, Querying & Ranking

### **query-language-spec.md**

Specification of the query language:
- grammar (BNF-style)
- AST structure
- operators (AND, OR, NOT)
- fields (tag:, mention:, type:, lang:, created:, pipeline:)
- wildcard rules
- FTS translation
- mention-aware graph lookups

### **ranking-and-reranking.md**

Retrieval strategies:
- scatter–gather
- deduplication
- weighted normalization
- Reciprocal Rank Fusion (RRF)
- registry-weighted scoring
- semantic clustering

---

## ⚙️ Configuration & Extensibility

### **configuration.md**

Covers:
- storage backends
- pipeline configuration
- LLM/embedding providers
- registries
- agents
- localization preferences
- plugin configuration
- polymorphic configuration blocks
- enabling/disabling mention features

### **plugins.md**

High-level plugin guide:
- plugin model
- configuration
- lifecycle hooks
- extending CLI and APIs
- creating pipelines
- registry providers
- custom metadata
- event listeners
- mention and graph augmentation rules

### **plugins-api.md**

Formal Plugin Interface Contract:
- lifecycle hooks
- permission system
- CLI/REST/gRPC extension rules
- pipeline extension API
- job scheduling API
- refresh API
- search operator extensions
- storage boundaries
- forbidden operations
- stability guarantees

### **plugins-refresh.md**

Plugin specification for refresh-style workflows:
- periodic ingestion
- refresh policies
- fetch-new behavior
- integration with job scheduling
- plugin safety rules

### **plugins-notifications.md**

Notification subsystem implemented as a plugin:
- channels
- routing
- persistence
- inter-plugin API
- CLI surface integration

### **plugins-sample-price-monitor.md**

A reference plugin used for tests:
- regex-based price monitoring
- alerting via notifications plugin
- refresh rule coordination
- user-defined thresholds
- bookmark-level metadata

---

## 🌐 APIs

### **api-cli.md**

Full CLI reference:
- analyze
- list
- jobs
- edit
- delete
- serve
- cache
- query examples
- plugin-injected commands
- mention resolution and retrieval

### **api-rest.md**

REST API reference:
- ingestion endpoints
- job management
- bookmark querying
- pagination
- registry operations
- entity lookup
- related entity traversal
- mention-aware search fields

### **api-grpc.md**

gRPC API:
- service definitions
- streaming endpoints
- high-performance agent integration
- entity metadata and graph queries

---

## 🌍 Localization & Internationalization

### **l10n-i18n.md**

Covers:
- localization plugins
- schema-level translation fields
- registry-provided translations
- language negotiation
- multi-language bookmark workflows

---

## 🛡️ Privacy, Security & Permissions

### **privacy.md**

Local-first guarantees:
- no data leaves device unless explicitly allowed
- registry and plugin access policies
- sensitive metadata handling

### **security.md**

Threat model:
- registry authenticity
- plugin permission enforcement
- sandbox rules
- signature validation
- entity spoofing protection

---

## 🔬 Testing & Quality

### **testing.md**

Comprehensive strategy:
- unit tests
- integration tests
- end-to-end tests
- ingestion crash recovery tests
- plugin tests
- query engine tests
- mention resolution tests
- graph correctness tests

---

## 🧩 Personas, Use Cases & Stories

### **personas-end-users.md**

Personas:
- researchers
- engineers
- analysts
- creators
- students

### **personas-user-roles.md**

User roles:
- end users
- developers
- plugin authors
- registry maintainers
- enterprise operators

### **user-stories.md**

Scenario-based documentation demonstrating use of:
- mentions
- entities
- plugins
- pipelines
- refresh workflows
- retrieval queries

---

## 🗂️ Additional Documents

Depending on development stage:

- **proposal.md** — Vision-level document.
- **caching.md** — Caching decorators.
- **sprints/** — Iteration planning.
- **decisions/** — Architectural Decision Records (ADRs).

---

## 📁 Directory Structure

```
docs
├── api-cli.md
├── api-grpc.md
├── api-rest.md
├── architecture.md
├── caching.md
├── configuration.md
├── decentralization.md
├── decisions
│   ├── ADR-001-local-first-and-decentralized.md
│   ├── ADR-002-use-go.md
│   ├── ADR-003-separate-read-write-paths.md
│   ├── ADR-004-step-based-pipeline.md
│   ├── ADR-005-decorator-pattern-for-ai.md
│   ├── ADR-006-json-default-storage.md
│   ├── ADR-007-transactional-outbox-ingestion.md
│   ├── ADR-008-remote-knowledge-registry-protocol.md
│   ├── ADR-009-multi-source-retrieval-optional-sync.md
│   ├── ADR-010-use-extended-rsql.md
│   ├── ADR-011-use-multi-source-reranker.md
│   ├── ADR-012-use-plugins-to-extend-any-layer.md
│   ├── ADR-013-knowledge-graph-and-mentions.md
│   ├── README.md
│   └── TEMPLATE.md
├── design.md
├── hints.md
├── jobs-and-ingestion.md
├── knowledge-graph.md
├── l10n-i18n.md
├── mentions.md
├── personas-end-users.md
├── personas-user-roles.md
├── pipelines-reference.md
├── pipelines.md
├── plugins-api.md
├── plugins-notifications.md
├── plugins-refresh.md
├── plugins-sample-price-monitor.md
├── plugins.md
├── privacy.md
├── query-language-spec.md
├── queue.md
├── ranking-and-reranking.md
├── README.md
├── registries.md
├── registry-protocol.md
├── registry-syncing-and-retrieval.md
├── schema-bookmark.md
├── schema-entity.md
├── schema-registry.md
├── schema-tag.md
├── schema-taxonomy.md
├── security.md
├── sprints
│   ├── 000-shared-kernel.md
│   ├── 001-echo-loop.md
│   ├── 002-real-data-and-queries.md
│   ├── 003-beyont-text.md
│   ├── 004-interface-and-independence.md
│   ├── 005-semantics-sidecars.md
│   ├── 006-polish-and-multimedia.md
│   ├── 007-sovereign-and-scalable.md
│   ├── 008-trust-and-automation.md
│   └── 009-proof-of-plarform.md
├── storage.md
├── tags.md
├── testing.md
├── user-roles.md
└── user-stories.md
```

---

## 🎯 How to Navigate

If you're new:

1. Start with **proposal.md** and **architecture.md**.
2. Continue with **pipelines.md**, **storage.md**, and **jobs-and-ingestion.md**.
3. Explore semantics: **mentions.md**, **schema-entity.md**, **schema-bookmark.md**, **knowledge-graph.md**.
4. Learn about extensibility in **plugins.md** and **plugins-api.md**.
5. For efficient storage: Review **leann-integration.md** (97% storage savings).
6. Use **api-cli.md**, **api-rest.md**, or **api-grpc.md** based on integration needs.

If you're contributing:

- Start with **CONTRIBUTING.md** (root project).
- Read **configuration.md**, **testing.md**, **plugins-api.md**, and relevant ADRs.

---

## 📩 Questions?

Discussion, RFCs, and design proposals are welcome via issues or PRs.
This documentation set evolves continuously — contributions are encouraged.