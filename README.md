# ContextHelp

**A decentralized, local-first context engine built on two packages:**
- **dPKMS** (substrate) - Safe execution, storage, graph, federation
- **ctxt** (brain) - Intelligence, pipelines, composition, surfacing

ContextHelp is a **semantic layer, not a replacement**. It augments Obsidian, Notion, Pocket, and similar
tools by adding stable entity identities, a queryable knowledge graph, and AI-ready retrieval on top of your
existing content — without asking you to abandon what already works.

It ingests text, URLs, images, audio, and video through **ctxt pipelines**; processes them via the **dPKMS
runtime**; normalizes meaning using **dPKMS registries** (taxonomies, entities, tags, weights); stores them as
structured **knowledge objects** in **dPKMS storage**; and exposes them to agents through the **ctxt CLI** and
**dPKMS APIs** (REST, gRPC).

ContextHelp provides not just storage, but a **semantic identity layer** (mentions + entities) and a
**knowledge graph** (dPKMS) connecting content and concepts. It is the foundational "context layer" for
personal and organizational AI — a system that captures, enriches, and retrieves knowledge in the user's
preferred language(s), fully under their control.

ContextHelp is self-hostable by default, and can optionally be used via **context.help cloud** for teams
that don't want to run infrastructure.

---

## DNS for Concepts

The **dPKMS registry** system works like DNS, but for knowledge:

- DNS maps `stripe.com` → IP address; dPKMS maps `@stripe.api` → canonical knowledge object
- Just as you subscribe to DNS resolvers, you subscribe to knowledge registries
- Registries resolve `@stripe.api` to a canonical entity with stable ID, aliases, tags, and relationships
- Any content mentioning `@stripe.api` automatically backlinks to that canonical entity
- Teams can share registries; communities can publish them; you can run your own

```
# DNS analogy
stripe.com  →  93.184.216.34          (hostname → IP)

# dPKMS registries
@stripe.api →  entity:payment/stripe  (concept → canonical knowledge)
              aliases: ["Stripe", "stripe-api"]
              tags: [payments, api, saas]
              related: [@stripe.webhooks, @stripe.elements]
```

This is what makes ctxt different from "RAG on your notes" — structured concept identity, not just fuzzy
text similarity.

---

## When NOT to Use ContextHelp

ContextHelp is deliberately scoped. It is **not** the right tool if:

- **You want a note-taking app** — use Obsidian or Notion. ContextHelp has no rich editor.
- **You need real-time collaboration** — use Confluence or Notion. ContextHelp is local-first, async by
  design.
- **Your notes don't have semantic complexity** — daily journals, to-do lists, and simple bookmarks don't
  benefit from entity resolution. The setup cost won't pay off.
- **You want zero-configuration** — Pocket and Raindrop are ready in 30 seconds. ContextHelp requires
  configuration (registries, pipelines, profiles).
- **You're looking for cloud-synced mobile notes** — ContextHelp's primary interface is CLI. Mobile is a
  plugin, not a first-class surface.
- **Your AI agent usage is occasional** — the value of a structured knowledge graph compounds over time.
  Light AI usage doesn't justify the overhead.

Use ContextHelp when you have **complex, interconnected knowledge** and need AI agents to reason over it
precisely — not when you need a simpler tool to do a simpler job.

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

**Docker (recommended for production):**

    git clone https://github.com/ideacrafterslabs/ctxt.git && cd ctxt
    cp docker/.env.prod.example .env.prod && $EDITOR .env.prod
    docker compose --profile prod up -d

See [docs/deployment/docker.md](docs/deployment/docker.md) for full details.

**Docker (local dev with hot-reload):**

    docker compose --profile dev up

See [docs/deployment/docker-dev.md](docs/deployment/docker-dev.md).

**Build from source:**

    task build
    ./bin/dpkms serve

If `task` is not on your shell `PATH`, run the same commands via `mise exec -- task ...`.

**Installation:** See [INSTALL.md](./INSTALL.md) for detailed installation instructions.

**Usage:** After installation, check the [Quick Start Guide](./docs/cli-quickstart.md) to begin using ContextHelp.

**Documentation:** Browse the complete documentation in [docs/README.md](./docs/README.md).

---

## Key Features (Pre-Alpha)

- **Universal Capture Layer**
  First-class capture from CLI. TUI, REPL, and Web UI are available as experimental surfaces. Browser extension and mobile share sheet are planned for future releases.

- **Multimodal Ingestion Pipelines** (Shipped)
  Text, URL, image, audio, and video processing with extensible steps, custom AI model integration, and entity-aware mention extraction.

- **Structured Knowledge Objects** (Shipped)
  Rich internal representation including summaries, tags, mentions, decisions, and embeddings, with full provenance tracking.

- **Semantic Identity & Knowledge Graph** (Shipped)
  Stable concepts (@mentions) and a queryable graph of relationships between content and entities.

- **Transactional Job Queue** (Shipped)
  Crash-safe, resumable ingestion and background processing using the local outbox pattern.

- **Advanced Query Language (RSQL)** (Shipped)
  Boolean logic, filters, and graph-aware operators translated to optimized SQL, FTS, and vector queries.

- **Action & Composition Layer** (Planned)
  Generation of briefs, plans, and publish-ready drafts from graph-connected atomic knowledge.

- **Evergreen Refresh Engine** (Planned)
  Policy-driven re-ingestion and scheduled review queues.

---

## CLI Overview

### `ctxt` (User-Facing Commands)

- `ctxt <content>` — Capture and enqueue content from arguments, stdin, or clipboard
- `ctxt analyze` — Capture and enqueue content for processing
- `ctxt list` — Query knowledge objects using RSQL filters
- `ctxt find <query>` — Semantic search across your knowledge base
- `ctxt open <id>` — View knowledge object details
- `ctxt jobs` — Inspect ingestion jobs and their status
- `ctxt tui` — (Experimental) Terminal User Interface
- `ctxt make` — (Planned) Generate compositions (briefs, plans, summaries)
- `ctxt profile` — (Planned) Manage focus profiles
- `ctxt registry` — (Planned) Manage registry subscriptions
- `ctxt uri register` — Register `ctxt://` as a clickable OS URL scheme

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
