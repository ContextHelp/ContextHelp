# dPKMS + `ctxt` – Detailed Design Documentation

This document consolidates the full set of detailed design documents for the **two-package architecture**:

- **dPKMS** — decentralized knowledge substrate (storage, jobs, security, federation)
- **`ctxt`** — agentic context brain (capture, enrichment, surfacing, composition)

It provides a comprehensive engineering blueprint including architecture, pipelines, jobs, storage, registries, extension surfaces, query language, ranking, configuration, I18N/L10N, APIs, security, and testing.

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

## High-Level Component Diagram

**dPKMS Components:**
- Storage Layer (SQLite/Postgres, pluggable)
- Job System (Transactional Outbox)
- Pipeline Runtime (Deterministic + Capability-Scoped)
- Query Engine (AST-based)
- Graph Index (Objects ↔ Entities)
- Registry Connectors (Federated)
- Encryption & Auth

**`ctxt` Components:**
- Capture Layer (CLI, TUI, Browser, Mobile)
- Enrichment Recipes (Multimodal Pipelines)
- Focus Profiles (Role/Project Lenses)
- Surfacing Engine (Just-In-Time)
- Composition Engine (Briefs, Plans, Drafts)
- Search UX

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
3. dPKMS detects query type (structured RSQL vs natural language).
4. **Branching:**
   - **If RSQL (agent/deterministic path):** Parse RSQL → AST → Query Plan (SQL-based metadata execution)
   - **If NLQ (human/semantic path):** Semantic normalizer → Intent analysis → Direct Query Plan (multi-strategy)
5. Query Plan dispatches to:
   - Local metadata search (SQL) — structured filters
   - Local FTS search (FTS5) — keyword/phrase matching
   - Vector search (optional) — semantic similarity
   - Graph traversal (entity-aware) — relationship discovery
   - Registry connectors (parallel scatter) — federated search
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

## Extension Points

There is no plugin system ([ADR-078](decisions/ADR-078-no-plugin-system.md)).
ctxt and dpkms are extended through pipeline overrides, custom pipelines,
steps installed from a step registry (out-of-process execution designed in
ADR-058, not wired yet), registries, and external hooks (ADR-075, not
implemented yet). Storage backends, encryption, query
operators, CLI commands, capture interfaces and observers are not extension
points.

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

Pipelines are changed through `pipelines.overrides` in the dpkms config and
through steps installed from a step registry (`dpkms pipeline step install`).

## AI Step Providers and Constrained Generation

When enrichment steps call AI models for structured extraction, constrained generation eliminates post-processing errors:

- **Tag Assignment** — constrained to registry vocabulary set (no ambiguous or out-of-vocabulary tags)
- **Mention/Entity Extraction** — regex-constrained to `@namespace.slug` format (always valid references)
- **Decision/Task Extraction** — dataclass/enum constraints on impact (LOW/MEDIUM/HIGH) and status (OPEN/RESOLVED/SUPERSEDED)
- **Pipeline Selector at Ingestion** — closed-set constraint over all registered pipeline names (always routes to valid handler)
- **Inline Branching** — alters extracted fields based on intermediate model decision without extra round-trip

**Constraint enforcement:** not implemented. Steps call the configured
`providers.*` backend and parse its output; there is no LMQL, instructor or
outlines integration and no token-level constraint.

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
- `pipeline` — Which pipeline processed this
- `source` — Origin URL/file/clipboard
- `registry_influences` — Which registries affected enrichment
- `created_at, updated_at` — Timestamps
- `fts_indexed` — Full-text index tracking flag

Vectors are not object columns: each registered embedding model stores its
vectors in the `embeddings` table, keyed `(object_id, model_id, chunk_idx)`
(ADR-071).

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

Defines the two query paths: RSQL for agents/deterministic queries, free-text search for humans (ADR-010 describes the original NLQ design; the AI normalizer it proposed is not implemented).

## Dual-Path Query Architecture

### Path 1: Structured RSQL (Agent/Deterministic)

Used by agents and systems requiring explicit, reproducible queries.

**Advantages:**
- Deterministic results (same query always produces same result order)
- Explicit intent (query is self-documenting)
- Single-strategy execution (SQL-based metadata filtering)
- Cacheable and auditeable

**Example:** `mentions=="user errors";type=in=summary,decision;created_at>2025-01-01`

### Path 2: Free-Text Search (Human)

Natural-language or keyword text goes to `ctxt find` (`POST /api/v1/find`).
There is no AI-backed NLQ normalizer: no intent classification, entity
extraction or AI strategy selection, and no LMQL/instructor/outlines constrained
generation. What runs instead:

- **Hybrid search (default):** FTS and vector legs fused with reciprocal rank
  fusion (RRF). `--fts` or `--semantic` runs one leg; `--fts-weight`,
  `--vector-weight`, `--rrf-k` and the pool sizes override the profile's
  `search_strategy` and the global `search` config.
- **Query clean-up:** common stopwords are stripped before the query is embedded.
- **Reranking:** the default reranker adds small mention, backlink and
  word-overlap bonuses on top of RRF order (`internal/ranking`).
- **Filters:** `--meta-type`, `--topic`, `--person`, `--source-type`, `--since`,
  `--until`, and the active focus profile.

`ctxt list --q "<rsql>"` runs an RSQL query instead.

**Agent Query Support:**

- Agents construct RSQL for deterministic queries (`ctxt list --q`), or call
  `ctxt find` for ranked free-text results.
- The dpkms MCP endpoint (`/api/v1/mcp`) exposes a `schema` tool (object kinds,
  edge types, pipelines) and a `search` tool.

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

## Query Plan Compilation & Execution

### RSQL Path (Structured)
- RSQL → AST (parser)
- AST → Query Plan (compiler)
- Single-strategy dispatch (primarily SQL metadata filtering)

### Free-Text Path
- Query → FTS leg + vector leg (dpkms embeds the query with its default model)
- RRF fusion, then reranking

### Query Plan Execution (Both Paths)

Each Query Plan may dispatch to multiple strategies in parallel:

- **Metadata Strategy** → SQL queries on object properties
- **FTS Strategy** → SQLite FTS5 queries on full-text content
- **Vector Strategy** → Vector store similarity search (optional)
- **Graph Strategy** → Entity-aware graph traversal
- **Registry Strategy** → Remote `/search` queries to federated registries

Result merging and reranking unifies outputs across strategies.

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

### Reranking in code

`internal/ranking.DefaultReranker` adds outbound-mention and inbound-backlink
boosts on top of the per-leg RRF scores. There is no AI-backed reranking: no
LMQL batch scoring, cross-encoder or pairwise LLM comparison, and no
pluggable ranking methods.

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

## Conflict Resolution

- Namespaced labels
- Canonical registry preference
- Alias resolution rules
- User override support

---

# extension surfaces

## Purpose

States how ctxt and dpkms are extended. Decision: no plugin system
([ADR-078](decisions/ADR-078-no-plugin-system.md), superseding ADR-012 and
ADR-027).

## What exists

- **Pipelines:** `pipelines.overrides.<pipeline>` (`providers`, `skip_steps`,
  `insert_steps`) and custom pipelines (`dpkms pipeline create`).
- **Steps:** built-in steps, plus installed steps (`dpkms pipeline step
  install`), run out of process (ADR-058) where `insert_steps` places them.
- **Registries:** entity, taxonomy and step registries.
- **Hooks:** pre and post hooks on ctxt and dpkms events (ADR-075, not implemented yet).

## What does not exist

No in-process plugin API, plugin registry, plugin manifest, `plugins:` config
key or AI-provider plugins (LMQL, instructor, outlines).

---

# configuration.md

## Purpose

Formalizes polymorphic config system (ADR-005, ADR-012).

## Config Loader

Each binary loads its own YAML file (`ctxt.yaml` or `dpkms.yaml`) through viper
and `hop.top/kit/go/core/config`: `/etc/contexthelp/<bin>.yaml`, then
`$XDG_CONFIG_HOME/contexthelp/<bin>.yaml`, then the nearest
`.contexthelp/<bin>.yaml` walking up from the working directory. `CTXT_CONFIG`
replaces the cascade; `-c/--config <file>` layers extra files and `-c key=value`
overrides single keys. A fixed list of env vars maps onto keys. There is no
`type:`-tag dispatch or external schema registration. Values are literal: `${VAR}`
is not expanded. `ctxt config validate` checks the result.

## Example Config

```yaml
storage:
  type: sqlite
  path: ./data/db.sqlite

# AI backends used by pipeline steps (tagging, sectioning, entity and
# metadata extraction). Keys come from OPENAI_API_KEY / ANTHROPIC_API_KEY
# (or the secrets backend), never from this file.
providers:
  llm:
    backend: openai          # openai | anthropic | ollama | stub | auto (empty)
    model: gpt-4o
  embedding:
    backend: ollama          # ollama (default) | stub
    model: nomic-embed-text
    endpoint: http://localhost:11434

registries:
  - name: uxpatterns
    url: https://uxpatterns.example.com
```

There is no `aiProvider`, `embeddingProvider` or `nlqNormalizer` key.

## Provider Configuration

`providers.<kind>` takes `backend`, plus optional `model`, `endpoint` and
`language`. Backends are a fixed set compiled into dpkms
(`internal/providers/factory.go`):

| Kind | Backends | Empty / `auto` |
|---|---|---|
| `llm` | `openai`, `anthropic`, `ollama`, `stub` | Anthropic if `ANTHROPIC_API_KEY` resolves, else OpenAI if `OPENAI_API_KEY` resolves, else Ollama at `http://localhost:11434` |
| `vision` | `ollama`, `openai`, `anthropic`, `gemini`, `openrouter`, `stub` | Ollama if reachable, else the first hosted key that resolves (OpenAI, Anthropic, Gemini, OpenRouter), else `stub` |
| `transcription` | `whisper-cli`, `ollama`, `stub` | `whisper-cli` if installed, else `stub` |
| `ocr` | `tesseract`, `stub` | `tesseract` if installed, else `stub` |
| `document` | `pdftotext`, `golib`, `stub` | `golib` (pure Go) |
| `video` | `ffmpeg`, `stub` | `ffmpeg` if installed, else `stub` |
| `diarization` | `pyannote`, `stub` | `pyannote` if installed, else `stub` |

`providers.embedding` takes `backend`, `model`, `endpoint`, `api_key_env` (the
name of the env var holding the key) and `dimension`; the effective embedding
provider is resolved per field from flags, env, `-c` overrides, the model
registry (`embeddings:`), this block and built-in defaults.

API keys are read from the secrets backend first, then the process environment:
`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OPENROUTER_API_KEY`.
Constrained generation (LMQL, instructor, outlines) and provider fallback chains
are not implemented.

## Environment Overrides

- `CH_STORAGE_TYPE=postgres`
- `OPENAI_API_KEY=<key>` (provider keys use their own names)

Language preferences have no env var: set `i18n.preferred_languages`.
Full list: [environment-variables/README.md](environment-variables/README.md).

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

## Step Responsibilities

- Language detection
- Translation API integration
- Cache handling
- Metadata augmentation

---

# api-cli.md

## Purpose

Specifies CLI interface.

## Commands

- `ch analyze` — Ingest and process content
- `ch analyze --lang <code> --translate none` — Override language detection
- `ch list` — List recent objects
- `ch search "<query>"` — Search (auto-detects RSQL or NLQ)
  - RSQL example: `ch search "type==article;tags=in=recommended"`
  - NLQ example: `ch search "show me recent recommended articles"`
- `ch query-schema` — List queryable properties and operators (for agents)
- `ch jobs list` — List background jobs
- `ch jobs status <id>` — Get job status
- `ch jobs retry <id>` — Retry failed job
- `ch sync <registry>` — Sync a federated registry
- `ch serve` — Start background daemon

## Flags

- `--type` — Hint content type (overrides detection)
- `--hints` — Provide pipeline hints
- `--file` — Read from file instead of stdin
- `--no-track` — Don't create job/bookmark (useful for testing)
- `--sort` — Sort results (relevance, date, custom)
- `--after` — Results after timestamp
- `--before` — Results before timestamp
- `--orig-lang` — Original language (for translation hints)
- `--prefer-lang` — Preferred output language
- `--query-mode` — Force query type (rsql or nlq)

---

# api-rest.md

## Purpose

Defines HTTP API.

## Endpoints

**Ingestion:**
- `POST /analyze` — Process and index content

**Retrieval:**
- `GET /search?q=...` — Search (auto-detects RSQL or NLQ)
- `GET /query-schema` — List queryable properties and operators (for agents)

**Object Management:**
- `GET /objects` — List objects
- `GET /objects/{id}` — Get specific object
- `DELETE /objects/{id}` — Delete object

**Job Management:**
- `GET /jobs` — List jobs
- `GET /jobs/{id}` — Get job status
- `POST /jobs/{id}/retry` — Retry failed job

**Registry Management:**
- `GET /registries` — List configured registries
- `POST /registries/{name}/sync` — Sync a registry

## Query Parameters (Search)

- `q` — Query (RSQL or natural language)
- `query_mode` — Force type (rsql, nlq, auto)
- `type` — Filter by object type
- `tag` — Filter by tags
- `pipeline` — Filter by pipeline
- `sort` — Sort order (relevance, date, custom)
- `after` — Results after timestamp
- `before` — Results before timestamp
- `lang` — Output language
- `origLang` — Original language hint
- `translate` — Translation mode (auto, none, force)
- `limit` — Results per page
- `offset` — Pagination offset

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

Defines security model for installed steps, registries, and local data.

## Threat Model

- malicious installed steps
- untrusted registries
- unsafe external commands
- sensitive local data exposure

## Controls

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
- **extension surfaces** — Pipelines, steps, registries and hooks for both layers
- **architecture.md** (semantic identity) — Entities + mentions + graph
- **configuration.md** (polymorphic config) — Pipeline and provider config

Together these form the **complete engineering blueprint** for the dPKMS + `ctxt` system.

**Key Insight:**
- dPKMS runs work **correctly**
- `ctxt` decides which work is **valuable**
- Together they provide **context-as-a-service** for humans and agents
