# Roadmap

ContextHelp is evolving from a robust local CLI tool into a decentralized, platform-agnostic knowledge engine. This roadmap outlines the path toward v1.0 and beyond, driven by our core principles: **Local-First**, **Decentralized**, **Deterministic**, **Plugin-Extensible**, and **Semantically Grounded** through **Mentions + Entities + Knowledge Graph**.

---

## Phase 1: Core Foundation (Current Focus)

**Goal:** Establish stable ingestion, deterministic pipelines, foundational storage, and core CLI/API interfaces while introducing the semantic identity substrate (mentions + canonical entities).

### Ingestion & Jobs (Transactional Outbox)

- [ ] Job Schema: Implement `jobs` and `job_steps` tables (ADR-007).
- [ ] Worker Implementation: `ch serve` background worker for processing pending jobs.
- [ ] Resilience: Automatic recovery of stale `Running` jobs on worker restart.
- [ ] Observability: `ch jobs logs <id>` to display full pipeline step traces.

### Storage & Querying

- [ ] SQLite Backend: WAL mode enabled (ADR-006).
- [ ] RSQL Parser: Full AST implementation for the Query Language (ADR-010).
- [ ] FTS Integration: SQLite FTS5 for text search.
- [ ] Basic Reranker: Local-only result merging.
- [ ] Mentions Storage: Add `mentions[]` array + entity index + backlinks table.
- [ ] Entity Support: Local definition support based on `schema-entity.md`.

### Pipelines (Built-in)

- [ ] Text Pipelines: `text.short` and `text.long`.
- [ ] URL Pipelines: `url.generic`, `url.repo`.
- [ ] Image Pipelines: `image.ocr`.
- [ ] Mention Extraction: Detect `@concept.id` syntax inside all modalities.
- [ ] Entity Resolution: Resolve mentions to canonical registry entities or local placeholders.

### Interfaces

- [ ] CLI: Core commands (`analyze`, `list`, `show`).
- [ ] REST API: Basic ingestion + retrieval endpoints.
- [ ] gRPC API: Services for ingestion, job status, and retrieval.
- [ ] Mention-aware Querying: Support `mention:` filters across CLI/REST/gRPC.

---

## Phase 2: Registry Ecosystem

**Goal:** Introduce decentralized knowledge distribution, multi-source retrieval, and consistent entity semantics across registries.

### Registry Protocol Implementation

- [ ] Client Adapter: HTTP client supporting Registry Protocol (ADR-008).
- [ ] Authentication: Token and header-based auth for private registries.
- [ ] Syncing: Implement schema-only and full sync modes (ADR-009).
- [ ] Merging Logic: Deterministic merging with local overrides.
- [ ] Entity Sync: Registries publish `entities[]` + aliases + translations.
- [ ] Alias Resolution: Registry → local fallback for entity ID resolution.

### Advanced Pipelines

- [ ] Audio/Video Pipelines: `audio.transcript`, `video.youtube`.
- [ ] Heuristics: Apply registry “weight” metadata to pipeline scoring.
- [ ] Hint Processing: Influence + interpretation logic for hints.
- [ ] Mention Promotion Workflow: Optional mechanism to turn hints into entities.

### Localization (I18N)

- [ ] Translation Plugins: Extension points for translation providers.
- [ ] Bookmark Translations: Add `translations` to bookmark schema.
- [ ] Content Negotiation: `preferLang` and related flags.
- [ ] Entity Translations: Multilingual entity labels from registries.

---

## Phase 3: Intelligence & Extensibility

**Goal:** Mature the plugin system, hybrid retrieval stack, and semantic navigation powered by the knowledge graph.

### Plugin System (ADR-012, Revised)

- [ ] Loader: Dynamic loading of Go plugins or RPC-based sidecars.
- [ ] Polymorphic Config: Plugin-defined configuration blocks.
- [ ] Pipeline Extensions: Plugins inject or override pipeline steps.
- [ ] Mention/Graph Augmentation: Plugins add edges, metadata, or aliases.
- [ ] Plugin API: Stable interfaces for job scheduling, refresh behavior, search operators, and metadata surfaces.

### Vector Search & Reranking

- [ ] Vector Store Interface: Pluggable embedding backend (SQLite-vss or external).
- [ ] Hybrid Search: RSQL filters + vector similarity.
- [ ] RRF Reranker: Reciprocal Rank Fusion for merging results (ADR-011).
- [ ] Entity Embeddings: Aggregate entity vectors from linked bookmarks.

### Agent Profiles

- [ ] Worldviews: Scoping of registries, tags, mentions, and entities per agent.
- [ ] Sandbox: Permission-controlled access for each agent.
- [ ] Graph-Aware Agents: Structured navigation through entities and backlinks.

---

## Phase 4: Scale & Enterprise

**Goal:** Enable distributed deployments, secure multi-user environments, and scalable graph operations.

### Advanced Storage & Queues

- [ ] Postgres Backend: Multi-user server-grade backing store.
- [ ] External Queues: Redis-based distributed job queue.
- [ ] Encryption: At-rest encryption for sensitive metadata and mentions.

### gRPC Streaming

- [ ] Live Ingestion: Streaming ingestion via `AnalyzeStream`.
- [ ] Search Cursors: Streamed retrieval for large datasets.
- [ ] Graph Streaming: Incremental streaming of backlinks and related entities.

### Registry Features

- [ ] Delta Sync: Efficient incremental registry updates.
- [ ] Discovery: Public registry discovery and catalog metadata.
- [ ] Entity Conflict Resolution: Support for version, alias, and namespace reconciliation.

---

## Phase 5: Future Horizons (Research)

**Goal:** Explore decentralized semantics, distributed learning, and advanced local inference.

- P2P Registries using IPFS or DID-based networks.
- WASM Plugins for safe, language-agnostic plugin execution.
- Local Model Fine-tuning using bookmark embeddings and metadata.
- Federated Search with trust-aware ranking and provenance trails.
- Temporal Knowledge Graph to model concept drift and semantic evolution.
- Entity Summaries using incremental, on-device generative models.

---

## Security & Privacy Audits

Performed continuously throughout all phases.

- [ ] Dependency Audits: Regular inspection of Go modules.
- [ ] Input Sanitization: Fuzz testing for HTML/Markdown pipelines.
- [ ] Registry Validation: Prevent malicious or spoofed entity definitions.
- [ ] Graph Integrity: Ensure entity identity stability and backlink consistency.
- [ ] Plugin Permissions: Verify plugin sandbox and access controls.

---

## Release Cadence

- **v0.5.x:** Completion of Phase 1 (Core + Mentions + Entity Scaffolding).
- **v0.6.x:** Registry support with entity sync and basic graph operations.
- **v0.8.x:** Plugin system maturity, hybrid retrieval, entity embeddings.
- **v1.0.0:** Stable APIs, storage format, registry protocol, plugin contract, and knowledge graph.