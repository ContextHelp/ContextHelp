# ContextHelp

ContextHelp is a decentralized, local-first context engine that transforms raw multimodal content into structured, contextualized knowledge consumable by humans and AI agents alike.

It ingests text, URLs, images, audio, and video; processes them through modular pipelines; normalizes meaning using registries (taxonomies, entities, tags, weights); stores them as structured bookmarks; and exposes them to agents through CLI, REST, and gRPC interfaces.

ContextHelp provides not just storage, but a **semantic identity layer** (mentions + entities) and a **knowledge graph** connecting content and concepts. It is the foundational “context layer” for personal and organizational AI — a system that captures, enriches, and retrieves knowledge in the user’s preferred language(s), fully under their control, without depending on any cloud services.

---

## Key Features

- **Multimodal Pipelines**
  Text, URL, image, audio, and video processing with extensible steps, custom AI models, mention extraction, entity resolution, and plugin-defined transformations.

- **Semantic Identity & Knowledge Graph**
  Stable entities (canonical concepts) referenced via `@mentions` in bookmarks, with a graph index of bookmark ↔ entity and entity ↔ entity edges used for querying, navigation, and agent reasoning.

- **Transactional Job Queue (Local Outbox Pattern)**
  All ingestion runs through a jobs table, guaranteeing safe recovery from crashes and enabling background pipeline execution.

- **Decentralized Registries**
  Subscribe to external or local sources for taxonomies, entity definitions, tag labels, weight systems, or shared bookmark knowledge — with optional authentication or paid access.

- **Structured Bookmarks**
  Each processed item becomes a fully enriched knowledge object with summaries, sections, tags, mentions, decisions, metadata, and provenance.

- **Advanced Query Language (AST-Based)**
  Supports boolean logic, filters, ranges, and nested expressions; translates cleanly into SQL, FTS, and vector queries, including mention-aware and graph-informed filters.

- **Scatter–Gather Retrieval**
  Query across local storage and multiple registries and merge results using a reranking layer designed for hybrid (symbolic + vector) search.

- **Plugin Architecture (With Formal Contract)**
  Plugins can extend pipelines, registries, commands, storage backends, query operators, AI providers, notification systems, refresh behavior, and more using a documented Plugin API and strict permission model.

- **L10N & I18N via Plugins**
  Users may configure preferred languages; plugins provide translation, localized entity and tag labels, and multilingual indexing.

- **Multiple Storage Backends**
  Default SQLite backend, with optional support for Postgres, remote stores, vector databases, or community-maintained “context management” systems.

---

## CLI Overview (`ch`)

The `ch` command-line interface provides:

- `ch analyze` — enqueue content for processing
- `ch list` — query bookmarks using filters or the query language
- `ch jobs` — inspect ingestion jobs and their status
- `ch serve` — run the background worker and expose APIs

Each command is backed by the same modular interfaces used by the REST and gRPC servers and can be augmented by plugins (additional commands, flags, and behaviors).

---

## Architecture at a Glance

- **Ingestion Path**
  CLI/API call → Job Stored → Worker Executes Pipelines (including mention extraction and entity resolution) → Bookmark Saved → Registries and Graph Updated

- **Read Path**
  Query → AST Parser → Storage Search + Registry Search + Graph Lookups → Reranker → Results

- **Isolation**
  Write path and read path are cleanly separated, preventing “God classes” and improving clarity, testability, and safety.

- **Caching Decorators**
  LLM and embedding clients are wrapped by decorators that inject caching transparently.

Refer to `docs/architecture.md` for a complete diagram and deeper technical details.

---

## Extensibility

ContextHelp is designed to be fully extensible through a **formal plugin interface**:

- New pipelines (custom ML models, custom steps, domain-specific ingestion)
- New CLI commands (UX helpers, automation commands, domain tools)
- New registries (knowledge providers, ontologies, commercial APIs)
- New storage backends (SQLite, Postgres, embedded KV stores, vector DBs)
- Workspace or OS integrations (screen-watching helpers, clipboard listeners)
- Agent-specific context providers and conditioning layers
- Semantic plugins (RSS feeds, auto-refresh, notifications, price monitors, etc.)

Plugins hook into well-defined lifecycle hooks and APIs (documented in `docs/plugins-api.md`) and operate in their own storage and metadata namespaces, without modifying core.

---

## Documentation

All documentation lives in the `docs/` directory.

Important starting points include:

- `docs/proposal.md` — high-level vision and problem statement
- `docs/architecture.md` — overall system design
- `docs/pipelines.md` — ingestion and pipeline architecture
- `docs/storage.md` — storage layer and bookmark model
- `docs/queue.md` and `docs/jobs-and-ingestion.md` — job system and ingestion lifecycle
- `docs/registries.md`, `docs/registry-protocol.md`, `docs/registry-syncing-and-retrieval.md` — decentralized registries
- `docs/mentions.md`, `docs/schema-entity.md`, `docs/knowledge-graph.md` — semantic identity and graph layer
- `docs/configuration.md` — configuration and environment
- `docs/plugins.md` and `docs/plugins-api.md` — plugin model and plugin contract
- `docs/api-cli.md`, `docs/api-rest.md`, `docs/api-grpc.md` — interfaces for integration
- `docs/testing.md` — testing strategy

Additional plugin-focused docs (examples and core plugins):

- `docs/plugins-refresh.md` — refresh/auto-fetch behavior
- `docs/plugins-notifications.md` — notification system
- `docs/plugins-sample-price-monitor.md` — sample price monitoring plugin

---

## License

ContextHelp is open-source under the **AGPL-3.0** license, ensuring that improvements remain part of the commons and that users always retain full control of their private context engine.