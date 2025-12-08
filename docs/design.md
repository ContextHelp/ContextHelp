# ContextHelp – Detailed Design Documentation

This document consolidates the full set of detailed design documents that complement the minimal ADR set. It provides a comprehensive engineering blueprint for the ContextHelp engine, including architecture, pipelines, jobs, storage, registries, plugins, query language, ranking, configuration, I18N/L10N, APIs, security, and testing.

---

# architecture.md

## Purpose

Describes the system’s structure, execution model, and major components. Expands on ADR-001, ADR-002, ADR-003.

## System Overview

ContextHelp is a **local-first knowledge engine** with optional decentralized registry integration. It operates in:

- **CLI mode**
- **Background worker** (`ch serve`)
- **HTTP/gRPC API server** mode

It processes content through pipelines, stores structured knowledge, and enables multi-source retrieval with ranking and deduplication.

## High-Level Component Diagram

Core components:

- CLI
- Job System (Transactional Outbox)
- Pipeline Engine
- Storage Layer (SQLite by default, pluggable)
- Search Layer (Metadata + FTS + optional vectors)
- Reranker
- Registry Connectors
- Plugin System

## Write Path

1. User runs `ch analyze`.
2. Engine **creates a Job** in `jobs` table (Pending).
3. CLI returns Job ID.
4. Worker picks up Pending jobs.
5. Worker executes pipeline steps.
6. Worker commits results to bookmarks table.
7. Job marked Completed.

## Read Path

1. Query parsed → AST.
2. AST compiled into a Query Plan.
3. Query Plan dispatches search to:
   - Local metadata search
   - Local FTS search
   - Optional vector store
   - Registry connectors (parallel scatter)
4. Results merged.
5. Reranker normalizes, dedupes, and returns final list.

## Plugin Integration Points

Plugins may extend:

- Pipelines
- AI Providers
- Storage Drivers
- CLI Commands
- Registry Connectors
- Query Operators
- Background Observers (screen watcher etc.)

---

# pipelines.md

## Purpose

Describes pipeline architecture, composition, extensibility, and guarantees (ADR-004, ADR-005).

## Pipeline Composition Model

Each pipeline consists of a **sequence of PipelineStep objects**. Pipelines transform raw input into structured bookmarks.

## PipelineStep Interface

Each step must implement:

- `Name()`
- `Execute(ctx, input) → (output, error)`
- `Retryable() bool`

## Pipeline Categories

- `text.short`
- `text.long`
- `url.generic`
- `url.repository`
- `image.landing`
- `audio.transcription`
- `video.transcription`

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

Describes storage layer, schema, and pluggable backends (ADR-006, ADR-022, ADR-024).

## Default Backend: SQLite

- WAL mode enabled
- Zero-install footprint
- Adequate performance for local-first workloads
- Strong FTS support

## Pluggable Storage Architecture

Drivers implement:

- `BookmarkStore`
- `JobStore`
- `TagStore`
- Optional `VectorStore`

## Bookmark Schema

Includes:

- id
- type, subtype
- raw content
- extracted summaries
- sections
- tags (canonical label, weight, polarity, influencedBy)
- hints
- decisions
- pipeline
- source
- createdAt
- updatedAt

## Index Strategy

- Metadata indexes on type, tag, createdAt, pipeline
- FTS indexes on text fields
- Optional vector index

## Migrations

- Managed via SQL migrations in a `migrations/` directory.

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

This consolidated design documentation includes:

- architecture.md
- pipelines.md
- jobs-and-ingestion.md
- storage.md
- query-language-spec.md
- ranking-and-reranking.md
- registries.md
- plugins.md
- configuration.md
- i18n-and-l10n.md
- api-cli.md
- api-rest.md
- api-grpc.md
- security.md
- testing.md

Together these form the **complete engineering blueprint** for ContextHelp.