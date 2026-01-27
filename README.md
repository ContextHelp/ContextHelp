# ContextHelp

**A decentralized, local-first context engine built on two packages:**
- **dPKMS** (substrate) - Safe execution, storage, graph, federation
- **ctxt** (brain) - Intelligence, pipelines, composition, surfacing

ContextHelp transforms raw multimodal content into structured, contextualized knowledge consumable by humans and AI agents alike.

It ingests text, URLs, images, audio, and video through **ctxt pipelines**; processes them via the **dPKMS runtime**; normalizes meaning using **dPKMS registries** (taxonomies, entities, tags, weights); stores them as structured **knowledge objects** in **dPKMS storage**; and exposes them to agents through the **ctxt CLI** and **dPKMS APIs** (REST, gRPC).

ContextHelp provides not just storage, but a **semantic identity layer** (mentions + entities) and a **knowledge graph** (dPKMS) connecting content and concepts. It is the foundational "context layer" for personal and organizational AI — a system that captures, enriches, and retrieves knowledge in the user's preferred language(s), fully under their control.

ContextHelp is self-hostable by default, and can optionally be used via **context.help cloud** for teams that don't want to run infrastructure.

---

## Two-Package Architecture

ContextHelp is built as two independent but cooperating packages:

### dPKMS (Substrate)
**The execution and data layer**

- Transactional job queue (crash-safe, resumable)
- Storage backends (SQLite, Postgres, plugin-provided)
- Knowledge graph (entities, mentions, backlinks)
- Query engine (RSQL → SQL compilation)
- Registry federation (sync, caching, merging)
- Security layer (encryption, permissions, signatures)
- REST + gRPC APIs

**Binary:** `dpkms`
**Focus:** Durability, correctness, sovereignty

### ctxt (Brain)
**The intelligence and behavior layer**

- Pipeline definitions (text, URL, image, audio, video)
- AI provider integrations (OpenAI, Anthropic, Ollama)
- Focus profiles (Founder, Engineer, Research)
- Multi-source retrieval and reranking
- Composition engine (briefs, plans, summaries)
- User-facing CLI commands

**Binary:** `ctxt`
**Focus:** Intelligence, usability, actionability

**Integration:** ctxt uses dPKMS to execute work safely. dPKMS provides capabilities; ctxt provides intent.

See `docs/branding.md` for naming conventions and `docs/architecture.md` for detailed architecture.

---

## Quick Start

**Installation:** See [INSTALL.md](./INSTALL.md) for detailed installation instructions.

**Usage:** After installation, check the [Quick Start Guide](./docs/quickstart-cli.md) to begin using ContextHelp.

**Documentation:** Browse the complete documentation in [docs/README.md](./docs/README.md).

---

## Key Features

- **Universal Capture Layer (Every Interface)**
  First-class capture from CLI, TUI, REPL, browser extension, web UI, and mobile share sheet, with offline-first behavior and a local outbox for seamless sync and processing.

- **Multimodal Pipelines**
  Text, URL, image, audio, video, documents, and code processing with extensible steps, custom AI models, mention extraction, entity resolution, and plugin-defined transformations.

- **Structured Knowledge Objects** (dPKMS storage, ctxt enrichment)
  Each processed item becomes a fully enriched knowledge object with summaries, extracted atomic notes, sections, tags, mentions, decisions, tasks, metadata, embeddings, and provenance.

- **Semantic Identity & Knowledge Graph**
  Stable entities (canonical concepts) referenced via `@mentions`, with a graph index of object ↔ entity and entity ↔ entity edges used for querying, navigation, relationship discovery, and agent reasoning.

- **Traceability + Version History**
  Full provenance preservation (source + time + pipeline + model) with revision history for objects, entities, and edges, including diff views, rollback, and “why this changed” explanations.

- **Transactional Job Queue (Local Outbox Pattern)**
  All ingestion and refresh operations run through a transactional jobs table, guaranteeing crash-safe recovery, resumable execution, and background pipeline execution.

- **Permissioned Sharing Model**
  Private-by-default workspaces with explicit permissions for publishing, subscribing, and collaboration at the level of collections, objects, entities, and views.

- **Decentralized Registries**
  Subscribe to external or local registries for taxonomies, entity definitions, tag labels, weight systems, shared knowledge packs, or workflows — with optional authentication or paid access (including index-only sync + just-in-time pulls for licensed content).

- **Advanced Query Language (AST-Based)**
  Supports boolean logic, filters, ranges, nested expressions, provenance constraints, and graph-aware operators; translates cleanly into SQL, FTS, vector queries, and traversal queries.

- **Federated Scatter–Gather Retrieval**
  Query across local storage and multiple registries and merge results using hybrid retrieval (symbolic + vector) with reranking informed by entities, provenance, and graph signals.

- **Evergreen Refresh Engine**
  Policy-driven refresh behaviors re-run pipelines on stale items, detect source changes, and resurface knowledge “just in time” via scheduled review queues and reminders.

- **Action Layer (Decisions, Tasks, Outputs)**
  Extract decisions, tasks, and next steps, and generate outputs like briefs, plans, checklists, meeting packets, and publish-ready drafts from graph-connected atomic knowledge.

- **Plugin Architecture (With Formal Contract)**
  Plugins can extend pipelines, registries, commands, storage backends, query operators, AI providers, UI surfaces, refresh policies, export formats, and notifications using a documented Plugin API and strict permission model.

- **Security + Encryption**
  Encryption at rest and in transit with pluggable key management, controlled sharing handshakes, and an optional privacy-preserving search mode that supports filtering and retrieval without exposing plaintext.

- **L10N & I18N via Plugins**
  Users may configure preferred languages; plugins provide translation, localized entity and tag labels, and multilingual indexing.

- **Multiple Storage Backends**
  Default SQLite backend, with optional support for Postgres, remote stores, vector databases, or community-maintained “context management” systems — all behind a portable storage contract.

- **Export + Portability Contract**
  Full export/import support using Markdown + JSON + SQLite bundles preserving entities, edges, provenance, and stable IDs, enabling migration without data loss or broken links.

- **Performance Guarantees**
  Optimized indexing for FTS + vectors + graph adjacency, incremental embedding updates, background compaction, and caching layers to keep search and retrieval “instant-feeling” at scale.

---

## CLI Overview

ContextHelp provides two command-line binaries:

### `ctxt` (User-Facing Commands)

- `ctxt analyze` — capture and enqueue content for processing
- `ctxt list` — query knowledge objects using filters or the query language
- `ctxt find` — semantic search across local and federated knowledge
- `ctxt open` — view structured knowledge object details
- `ctxt jobs` — inspect ingestion jobs and their status
- `ctxt profile` — manage focus profiles (Founder, Engineer, Research, etc.)
- `ctxt make` — generate compositions (briefs, plans, summaries)
- `ctxt registry` — manage registry subscriptions

### `dpkms` (Infrastructure Commands)

- `dpkms serve` — run the background worker and expose REST/gRPC APIs
- `dpkms housekeeping` — database maintenance, compaction, and optimization

Each command is backed by the same modular interfaces used by the REST and gRPC servers and can be augmented by plugins (additional commands, flags, and behaviors).

See `docs/ctxt/api-cli.md` for complete CLI reference.

---

## Architecture at a Glance

### Ingestion Path (ctxt → dPKMS)
```
ctxt analyze
  ↓ (pipeline selection)
ctxt enqueues job → dPKMS job queue
  ↓
dpkms serve picks up job → dPKMS worker
  ↓ (executes ctxt-defined pipeline)
Mention extraction (ctxt) → Entity resolution (dPKMS)
  ↓
Knowledge Object saved → dPKMS storage
  ↓
Graph edges updated → dPKMS knowledge graph
```

### Read Path (ctxt → dPKMS)
```
ctxt list --q "query"
  ↓
dPKMS query engine: RSQL → AST → SQL
  ↓
dPKMS storage + registry federation + graph traversal
  ↓
Results → ctxt reranking (RRF, vector similarity)
  ↓
Formatted output to user
```

### Package Separation
- **ctxt** decides what to do (pipeline selection, enrichment rules, output format)
- **dPKMS** ensures it's done safely (jobs, storage, graph, query, federation)
- Clean interfaces enable independent evolution

Refer to `docs/architecture.md` for complete diagrams and `docs/sprints/CROSS-PACKAGE-CONTRACTS.md` for integration details.

---

## Extensibility

ContextHelp is designed to be fully extensible through a **formal plugin interface**:

### dPKMS Plugins
- Storage backends (SQLite, Postgres, embedded KV stores, vector DBs)
- Queue backends (Redis, memory, custom)
- Registry providers (custom protocols, authenticated sources)
- Security providers (encryption, key management)

### ctxt Plugins
- Pipeline steps (custom ML models, domain-specific processing)
- AI providers (custom LLMs, embeddings)
- Reranking algorithms (custom scoring, filtering)
- Output formatters (custom templates, export formats)

### Cross-Package Plugins
- CLI commands (UX helpers, automation, domain tools)
- Workspace integrations (clipboard watchers, directory monitors)
- Semantic plugins (RSS feeds, auto-refresh, notifications, price monitors)

Plugins hook into well-defined lifecycle hooks and APIs (documented in `docs/plugins/plugins-api.md`) and operate in their own storage and metadata namespaces, without modifying core.

See `docs/sprints/CROSS-PACKAGE-CONTRACTS.md` for plugin integration points.

---

## Documentation

All documentation lives in the `docs/` directory, organized by package:

### Getting Started
- `docs/README.md` — Documentation index with role-based navigation
- `docs/architecture.md` — Two-package system architecture (dPKMS + ctxt)
- `docs/branding.md` — Naming conventions (ContextHelp, dPKMS, ctxt)
- `docs/dpkms-or-ctxt.md` — Package placement guide
- `ROADMAP.md` — Living skeleton development roadmap

### dPKMS (Substrate) Documentation
- `docs/dpkms/storage.md` — Storage backends and persistence
- `docs/dpkms/jobs-and-ingestion.md` — Job queue and worker runtime
- `docs/dpkms/query-language-spec.md` — RSQL query language
- `docs/dpkms/knowledge-graph.md` — Entity graph and backlinks
- `docs/dpkms/mentions.md` — Mention system (`@namespace.slug`)
- `docs/dpkms/schema-entity.md` — Entity schema specification
- `docs/dpkms/registries.md` — Registry federation
- `docs/dpkms/registry-protocol.md` — Registry HTTP protocol
- `docs/dpkms/security.md` — Encryption and permissions
- `docs/dpkms/testing.md` — dPKMS testing strategy

### ctxt (Brain) Documentation
- `docs/ctxt/api-cli.md` — CLI command reference
- `docs/ctxt/pipelines.md` — Pipeline architecture and recipes
- `docs/ctxt/pipelines-reference.md` — Complete pipeline catalog
- `docs/ctxt/schema-object.md` — Knowledge object schema
- `docs/ctxt/tags.md` — Tag semantics
- `docs/ctxt/configuration.md` — Focus profiles and preferences
- `docs/ctxt/user-stories.md` — User stories and workflows
- `docs/ctxt/testing.md` — ctxt testing strategy

### Plugin System (Cross-Cutting)
- `docs/plugins/plugins.md` — Plugin architecture
- `docs/plugins/plugins-api.md` — Plugin API contract
- `docs/plugins/plugins-notifications.md` — Notification plugin
- `docs/plugins/plugins-refresh.md` — Refresh plugin
- `docs/plugins/examples/` — Sample plugin implementations

### API Documentation
- `docs/api/api-rest.md` — REST API reference
- `docs/api/api-grpc.md` — gRPC API reference

### Sprint Documentation
- `docs/sprints/README.md` — Living skeleton sprint index
- `docs/sprints/CROSS-PACKAGE-CONTRACTS.md` — dPKMS ↔ ctxt integration
- `docs/sprints/CONFIGURATION-STRUCTURE.md` — Config file organization
- `docs/sprints/000-shared-kernel.md` through `009-proof-of-plarform.md` — Sprint plans

---

## License

ContextHelp is open-source under the **AGPL-3.0** license, ensuring that improvements remain part of the commons and that users always retain full control of their private context engine.
