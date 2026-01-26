# dPKMS + `ctxt` – Detailed Design Documentation

This document consolidates the full set of detailed design documents for the **two-package architecture**:

- **dPKMS** — decentralized knowledge substrate (storage, jobs, security, federation)
- **`ctxt`** — agentic context brain (capture, enrichment, surfacing, composition)

It provides a comprehensive engineering blueprint including architecture, pipelines, jobs, storage, registries, plugins, query language, ranking, configuration, I18N/L10N, APIs, security, and testing.

This design separates **mechanics** (dPKMS) from **meaning** (`ctxt`).

---

# architecture.md

## Purpose

Describes the two-package system structure, execution model, and major components. Expands on ADR-001, ADR-002, ADR-003.

## System Overview

The system consists of two packages:

**dPKMS** — A local-first knowledge substrate providing:
- Durable storage with pluggable backends
- Transactional job queue and pipeline runtime
- Semantic identity (entities + mentions + graph)
- Encryption, authentication, authorization
- Federated registries and scatter–gather retrieval
- Export/import portability contract

**`ctxt`** — An agentic context brain providing:
- Universal capture (CLI, TUI, browser, mobile)
- Multimodal enrichment recipes
- Focus profiles (role/project lenses)
- Just-in-time surfacing
- Composition engine (briefs, plans, drafts)
- Safe agent execution workflows

**Execution Modes:**
- `ctxt` CLI (primary user interface)
- `dpkms serve` (background worker daemon)
- HTTP/gRPC API server (for integrations)
- Plugin-extended modes

## High-Level Component Diagram

**dPKMS Components:**
- Storage Layer (SQLite/Postgres, pluggable)
- Job System (Transactional Outbox)
- Pipeline Runtime (Deterministic + Capability-Scoped)
- Query Engine (AST-based)
- Graph Index (Objects ↔ Entities)
- Registry Connectors (Federated)
- Encryption & Auth
- Plugin System (dPKMS layer)

**`ctxt` Components:**
- Capture Layer (CLI, TUI, Browser, Mobile)
- Enrichment Recipes (Multimodal Pipelines)
- Focus Profiles (Role/Project Lenses)
- Surfacing Engine (Just-In-Time)
- Composition Engine (Briefs, Plans, Drafts)
- Search UX
- Plugin System (`ctxt` layer)

## Write Path (Ingestion)

1. User runs `ctxt add <content>`.
2. `ctxt` normalizes input and infers type/pipeline.
3. `ctxt` applies focus profile context.
4. dPKMS **creates a Job** in `jobs` table (Pending).
5. `ctxt` returns immediately (async).
6. dPKMS worker picks up Pending jobs.
7. dPKMS executes pipeline steps (via `ctxt` enrichment recipes).
8. dPKMS commits results to knowledge objects table.
9. dPKMS updates graph index (mentions, entities, edges).
10. Job marked Completed.

## Read Path (Retrieval)

1. User runs `ctxt find <query>`.
2. `ctxt` applies focus profile context.
3. dPKMS parses query → AST.
4. dPKMS compiles AST → Query Plan.
5. Query Plan dispatches to:
   - Local metadata search (SQL)
   - Local FTS search (FTS5)
   - Vector search (optional)
   - Graph traversal (entity-aware)
   - Registry connectors (parallel scatter)
6. dPKMS merges results.
7. dPKMS reranks and deduplicates.
8. `ctxt` applies profile filtering and boost.
9. `ctxt` formats and displays results.

## Composition Path

1. User runs `ctxt make brief --from <ids>`.
2. `ctxt` selects template based on profile and type.
3. `ctxt` gathers context (objects + graph neighbors).
4. dPKMS retrieves objects, entities, edges.
5. `ctxt` assembles atomic nodes following graph.
6. `ctxt` applies template and generates output.
7. `ctxt` attaches provenance metadata.
8. User receives traceable, verifiable brief.

## Plugin Integration Points

**dPKMS Plugin Points:**
- Storage backends
- Encryption providers
- Query operators
- Registry connectors
- Graph algorithms
- Job types
- Pipeline runtime hooks

**`ctxt` Plugin Points:**
- Capture interfaces
- Enrichment recipes
- Pipeline steps
- Focus profiles
- Surfacing rules
- Composition templates
- CLI commands
- Output formatters

**Cross-Layer Plugin Points:**
- AI providers (used by both)
- Background observers (screen watcher, etc.)
- Notification handlers
- Export/import formats

---

# pipelines.md

## Purpose

Describes the two-layer pipeline architecture:
- **dPKMS Pipeline Runtime** (execution substrate)
- **`ctxt` Enrichment Recipes** (opinionated workflows)

Covers composition, extensibility, and guarantees (ADR-004, ADR-005).

## Two-Layer Pipeline Model

**Layer 1: dPKMS Pipeline Runtime**

Provides the execution substrate:
- Step isolation and typed I/O
- Capability enforcement
- Caching and idempotency
- Structured logging
- Deterministic replay

**Layer 2: `ctxt` Enrichment Recipes**

Provides opinionated processing workflows:
- Multimodal ingestion recipes
- AI-driven enrichment steps
- Profile-aware pipeline selection
- Progressive enrichment strategies

Pipelines transform raw input into structured knowledge objects.

## PipelineStep Interface

Each step must implement:

- `Name()`
- `Execute(ctx, input) → (output, error)`
- `Retryable() bool`

## Pipeline Categories (`ctxt` Recipes)

**Text Pipelines:**
- `text.short` — Quick summarization, entity extraction, tag assignment
- `text.long` — Deep analysis, section decomposition, decision extraction

**URL Pipelines:**
- `url.generic` — Fetch, clean HTML, extract article content
- `url.repository` — Clone repo, parse README, extract structure
- `url.article` — Article extraction with reader mode
- `url.pdf` — Download and process PDF

**Image Pipelines:**
- `image.ocr` — OCR text extraction
- `image.landing` — Screenshot analysis, UI pattern detection
- `image.diagram` — Diagram understanding, entity extraction

**Audio Pipelines:**
- `audio.transcription` — Speech-to-text, speaker detection
- `audio.podcast` — Podcast processing with chapters

**Video Pipelines:**
- `video.transcription` — Frame sampling + audio transcription
- `video.analysis` — Scene detection, visual understanding

**Document Pipelines:**
- `document.pdf` — Text extraction, structure parsing
- `document.markdown` — Parse and decompose Markdown
- `document.code` — Code snippet analysis

**Specialized Pipelines:**
- `feed.item` — RSS/Atom item processing
- `price.monitor` — Price tracking and alerts
- `task.extraction` — Task and action item detection
- `decision.extraction` — Decision point identification

## Custom Pipelines

Plugins may register:

- New pipeline step types
- Entire pipelines
- Conditional branching logic

## Execution Guarantees

- Idempotent
- Deterministic (same inputs → same outputs)
- Safe to retry
- No side-effects except final write
- Traceable through `job_steps`

---

# jobs-and-ingestion.md

## Purpose

Implements transactional outbox job system for ingestion reliability (ADR-003, ADR-007).

## Job Lifecycle

States:

- Pending
- Running
- Completed
- Failed

## Job Table Schema

Fields:

- job_id
- input payload
- pipeline type
- job state
- retry count
- timestamps
- error messages (optional)

## Job Execution

Worker:

1. Picks Pending jobs.
2. Moves to Running.
3. Executes pipeline.
4. Writes bookmark.
5. Marks job Completed.

## Resilience Mechanisms

- Stale Running jobs reset to Pending (timeout-based).
- Configurable retry count.
- Failed jobs kept for inspection.
- Worker crash recovery is automatic via job state fields.

## CLI + API

- `ch jobs list`
- `ch jobs status <id>`
- `ch jobs retry <id>`

---

# storage.md

## Purpose

Describes the dPKMS storage layer, schema, and pluggable backends (ADR-006, ADR-022, ADR-024).

## Default Backend: SQLite

**Why SQLite:**
- WAL mode for concurrency
- Zero-install footprint
- Excellent local-first performance
- Strong FTS5 support
- Reliable and battle-tested
- Perfect for sovereign storage

**Configuration:**
- `PRAGMA journal_mode=WAL`
- `PRAGMA synchronous=NORMAL`
- `PRAGMA cache_size=-64000` (64MB)
- `PRAGMA foreign_keys=ON`

## Pluggable Storage Architecture

Storage drivers implement dPKMS capability contracts:

**Core Store Interfaces:**
- `ObjectStore` — Knowledge objects (successor to BookmarkStore)
- `JobStore` — Job queue operations
- `GraphStore` — Entity and edge storage
- `EntityStore` — Canonical entity definitions
- `VectorStore` — Optional embedding storage

## Knowledge Object Schema (Successor to "Bookmarks")

Core fields:

- `id` — UUID, stable across exports
- `type, subtype` — Classification
- `raw_content` — Original input
- `content_type` — MIME type
- `metadata` — JSON extensible metadata
- `summaries` — JSON array of summaries
- `sections` — JSON array of decomposed sections
- `tags` — JSON array with weights and sources
- `mentions` — JSON array of `@entity.slug` references
- `decisions` — JSON array of extracted decisions
- `tasks` — JSON array of actionable items
- `embeddings` — Vector representation (BLOB)
- `pipeline` — Which pipeline processed this
- `source` — Origin URL/file/clipboard
- `registry_influences` — Which registries affected enrichment
- `created_at, updated_at` — Timestamps
- `fts_indexed, vector_indexed` — Index tracking flags

## Additional Tables

**Entities:**
- `id` (canonical slug)
- `title, description`
- `aliases` (JSON array)
- `translations` (JSON map)
- `metadata` (JSON)
- `namespace, version`
- `registry_source`

**Graph Edges:**
- `id` (UUID)
- `from_type, from_id`
- `to_type, to_id`
- `edge_type` (mentions, related, derives_from)
- `weight`
- `metadata` (JSON)

**Jobs:** (see jobs-and-ingestion.md)

**Registry Snapshots:** (see registries.md)

**Revisions:** Version history tracking

## Index Strategy

**Metadata Indexes:**
- `type, subtype`
- `tags` (JSON index)
- `created_at, updated_at`
- `pipeline, source`

**FTS Indexes:**
- FTS5 virtual table on summaries, sections, raw_content
- Porter stemming + Unicode61 tokenization

**Vector Indexes:**
- Optional pluggable vector backend
- Supports similarity search

**Graph Indexes:**
- Adjacency indexes on edges table
- Bidirectional traversal support

## Migrations

- Managed via SQL migrations in `migrations/` directory
- Stable ID preservation across versions
- Reversible migration paths with rollback support
- Version tracking in schema_version table

---

# query-language-spec.md

## Purpose

Defines RSQL-based query language with ContextHelp extensions (ADR-010).

## RSQL Core Operators

- Equality: `==`
- Inequality: `!=`
- Range: `<`, `>`, `>=`, `<=`
- IN / OUT: `=in=`, `=out=`
- Logical AND: `;`
- Logical OR: `,`
- Parentheses for grouping

## Extended Operators

- Pipeline filter:
  `pipeline==text.short`
- Registry filter:
  `registry==uxpatterns`
- Language filter:
  `lang==en`
- Semantic similarity:
  `similar=="signup flow"`

## AST Model

Nodes:

- ComparisonNode
- LogicalAndNode
- LogicalOrNode
- NotNode
- ValueNode
- FieldNode

## Query Plan Mapping

- Metadata → SQL
- FTS → SQLite FTS query
- Vectors → vector store search
- Registries → remote `/search` queries
- Reranker merges results

---

# ranking-and-reranking.md

## Purpose

Explains merging of results across local and remote sources (ADR-009, ADR-011).

## Why Reranking

Different sources produce incompatible scores; results must be unified.

## Supported Algorithms

- Reciprocal Rank Fusion (RRF)
- Weighted Sum Ranking
- SoftMax normalization
- Plugin-provided ranking methods

## Deduplication Strategy

- Exact ID match
- URL normalization
- Semantic fingerprint match (embedding cosine score)

## Registry Weighting

Registries may supply:

- default search weight
- domain specificity score
- trust/credibility metadata

## Explainability

Each result includes a `rank.explain` payload describing:

- contributing factors
- weighted components
- source weighting

---

# registries.md

## Purpose

Specifies registry protocol & behavior (ADR-008, ADR-009).

## Registry Types

- Taxonomy registry
- Tag registry
- Bookmark registry
- Multi-purpose registry

## Required Endpoints

- `/tags`
- `/taxonomy`
- `/vocabulary`
- `/search`
- `/metadata`

## Optional Endpoints

- `/sync/snapshot`
- `/sync/delta`
- `/assets`
- `/embeddings`

## Auth Models

- API key
- OAuth device flow
- Plugin-driven authentication

## Conflict Resolution

- Namespaced labels
- Canonical registry preference
- Alias resolution rules
- User override support

---

# plugins.md

## Purpose

Defines plugin model and lifecycle (ADR-012).

## Goals

- Extensible in all major system layers
- Safe execution
- Explicit user permissions

## Plugin Types

- Pipeline extension
- Storage backend
- Registry adapter
- Query operators
- AI provider
- Background observer (screen watcher)
- Translation provider
- Alias/command enhancer

## Plugin Interface

Plugins expose:

```
Register(HostContext)
```

Where HostContext allows registering:

- pipelines
- commands
- operators
- registry connectors
- config schemas

## Permission Model

User must explicitly approve:

- screen capture
- clipboard monitoring
- network calls
- modifying bookmarks
- filesystem writes

## Configuration

Polymorphic config loading:

```
myplugin:
  type: plugin
  plugin: "./plugins/myplugin.so"
  config:
    foo: bar
```

## Packaging

- Static linking
- `.so` dynamic modules
- Distributed through registries or GitHub

---

# configuration.md

## Purpose

Formalizes polymorphic config system (ADR-005, ADR-012).

## Config Loader

Loads YAML/JSON with:

- `type` tag dispatching
- plugin schema registration
- environment variable overrides

## Example Config

```
storage:
  type: sqlite
  path: ./data/db.sqlite

embeddingProvider:
  type: openai
  model: text-embedding-3-large

registries:
  - name: uxpatterns
    url: https://uxpatterns.example.com

plugins:
  - type: plugin
    plugin: ./plugins/screensuggester.so
```

## Environment Overrides

- `CH_STORAGE_TYPE=postgres`
- `CH_OPENAI_API_KEY=<key>`
- `CH_PREF_LANG=fr`

---

# i18n-and-l10n.md

## Purpose

Defines optional localization/internationalization strategy.

## Multi-Language Model

Original content stored unchanged.
Translations stored under:

```
translations:
  fr:
    summary: "..."
    tags:
      - label: ...
```

## Translation Layers

Possible to translate:

- summary
- sections
- tag labels
- decisions
- title

## User Preferences

Example:

```
i18n:
  enabled: true
  preferredLanguages: ["fr", "en"]
  autoTranslate: true
  translateTags: true
  skipOnAnalyzeDefault: false
```

## Plugin Responsibilities

- Language detection
- Translation API integration
- Cache handling
- Metadata augmentation

---

# api-cli.md

## Purpose

Specifies CLI interface.

## Commands

- `ch analyze`
- `ch analyze --lang <code> --translate none`
- `ch list`
- `ch search "<query>"`
- `ch jobs list`
- `ch jobs status <id>`
- `ch sync <registry>`
- `ch serve`

## Flags

- `--type`
- `--hints`
- `--file`
- `--no-track`
- `--sort`
- `--after`
- `--before`
- `--orig-lang`
- `--prefer-lang`

---

# api-rest.md

## Purpose

Defines HTTP API.

## Endpoints

- `POST /analyze`
- `GET /jobs`
- `GET /jobs/{id}`
- `GET /bookmarks`
- `GET /search?q=...&lang=...&translate=...`
- `GET /registries`
- `POST /sync/{registry}`

## Query Parameters

- `type`
- `tag`
- `pipeline`
- `sort`
- `after`
- `before`
- `lang`
- `origLang`
- `translate`

---

# api-grpc.md

## Purpose

Defines gRPC API and message contracts.

## Services

- `Analyze`
- `ListBookmarks`
- `Search`
- `GetJob`
- `ListJobs`
- `SyncRegistry`

## Features

- streaming results supported
- metadata extension fields
- language preferences

---

# security.md

## Purpose

Defines security model for plugins, registries, and local data.

## Threat Model

- malicious plugins
- untrusted registries
- unsafe external commands
- sensitive local data exposure

## Controls

- explicit permissions for plugins
- sandbox-like execution
- registry authentication
- configurable network restrictions
- optional storage encryption

---

# testing.md (Extended)

## Purpose

Ensures correctness, reliability, and safety.

## Test Categories

- Unit tests (mocked AI & storage)
- Pipeline tests
- Job retry tests
- FTS integration tests
- Registry mocks
- Query parser tests
- Reranker tests
- I18N translation tests

## E2E Tests

- analyze → job → worker → bookmark
- search (local + registry)
- sync (when enabled)

---

# Summary

This consolidated design documentation covers the **two-package architecture**:

## dPKMS (Substrate)

Provides mechanical guarantees:
- **storage.md** — Durable, sovereign, local-first storage
- **jobs-and-ingestion.md** — Transactional job queue
- **pipelines.md** (runtime layer) — Safe pipeline execution
- **query-language-spec.md** — AST-based query engine
- **ranking-and-reranking.md** — Federated result merging
- **registries.md** — Decentralized knowledge distribution
- **security.md** — Encryption, auth, integrity
- **testing.md** — Correctness and reliability

## `ctxt` (Agentic Brain)

Provides meaningful behaviors:
- **architecture.md** (capture layer) — Universal frictionless capture
- **pipelines.md** (recipes layer) — Multimodal enrichment
- **api-cli.md** — Daily-use interface
- **api-rest.md** — Integration endpoints
- **api-grpc.md** — Agent runtime APIs
- **i18n-and-l10n.md** — Polyglot support
- **configuration.md** — Focus profiles and preferences

## Shared

Cross-cutting concerns:
- **plugins.md** — Extensibility for both layers
- **architecture.md** (semantic identity) — Entities + mentions + graph
- **configuration.md** (polymorphic config) — Plugin and provider config

Together these form the **complete engineering blueprint** for the dPKMS + `ctxt` system.

**Key Insight:**
- dPKMS runs work **correctly**
- `ctxt` decides which work is **valuable**
- Together they provide **context-as-a-service** for humans and agents