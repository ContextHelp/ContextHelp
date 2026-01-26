# Cross-Package Contracts (dPKMS ↔ ctxt)

This document defines the boundaries and contracts between dPKMS (substrate) and ctxt (brain) as they relate to the Living Skeleton sprints.

---

## Package Responsibilities

### dPKMS (Substrate)

**What dPKMS owns:**
- Job queue (transactional outbox, crash recovery)
- Storage backends (SQLite, Postgres, plugin-provided)
- Schema and migrations
- Query engine (RSQL → SQL transpilation)
- Knowledge graph (entities, mentions, backlinks)
- Registry protocol (HTTP client, sync, caching)
- Security (encryption, permissions, signatures)
- Pipeline **runtime** (execution, isolation, step tracking)

**Binary:** `dpkms`
**Example commands:**
- `dpkms serve` - Run background worker
- `dpkms housekeeping` - Database maintenance

### ctxt (Brain)

**What ctxt owns:**
- Pipeline **definitions** (what steps, what order)
- AI provider integrations (OpenAI, Anthropic, local models)
- Job type definitions ("ingest:url", "enrich:audio")
- Multi-source retrieval logic and reranking
- User-facing CLI commands
- Focus profiles (Founder, Engineer, Research, etc.)
- Composition engine (briefs, plans, summaries)

**Binary:** `ctxt`
**Example commands:**
- `ctxt analyze` - Capture input
- `ctxt list` - Search and list
- `ctxt open` - View knowledge object
- `ctxt jobs` - Monitor jobs (wrapper around dPKMS job queue)

---

## Cross-Package Integration Points

### 1. Jobs (ctxt → dPKMS)

**Contract:**
- ctxt defines job types and enqueues jobs
- dPKMS executes jobs and tracks state

**Flow:**
```
User: ctxt analyze "text"
  ↓
ctxt: determines pipeline (text.short)
  ↓
ctxt: creates Job { type: "ingest:text", pipeline: "text.short", input: "..." }
  ↓
dPKMS: enqueues job → Pending
  ↓
dpkms serve: worker picks up job → Running
  ↓
dPKMS: executes ctxt-defined pipeline steps
  ↓
dPKMS: writes KnowledgeObject to storage
  ↓
dPKMS: job → Completed
```

**Interface (dPKMS):**
```go
type JobQueue interface {
    Enqueue(ctx context.Context, job Job) (string, error)
    AcquireNext(ctx context.Context) (*Job, error)
    UpdateStatus(ctx context.Context, id string, status JobStatus) error
}
```

**Interface (ctxt):**
```go
type PipelineDefinition interface {
    Name() string
    Steps() []PipelineStep
    CanHandle(input Input) bool
}
```

---

### 2. Mentions & Entities (ctxt ↔ dPKMS)

**Contract:**
- ctxt extracts mentions from content
- dPKMS stores mentions and maintains graph
- dPKMS resolves entities via registry lookups
- Both use canonical entity IDs

**Flow:**
```
ctxt pipeline: analyzes text → finds "@ui.best-practice"
  ↓
ctxt: emits mention candidate
  ↓
dPKMS: resolves mention via entity index or registry
  ↓
dPKMS: creates/updates entity record
  ↓
dPKMS: creates backlink (knowledge_object → entity)
  ↓
dPKMS: stores in knowledge_objects table with mentions: ["ui.best-practice"]
```

**Interface (dPKMS):**
```go
type EntityStore interface {
    UpsertEntity(ctx context.Context, entity Entity) error
    ResolveEntity(ctx context.Context, mention string) (*Entity, error)
    AddBacklink(ctx context.Context, entityID, objectID string) error
}
```

**Interface (ctxt):**
```go
type MentionExtractor interface {
    Extract(ctx context.Context, text string) ([]Mention, error)
}
```

---

### 3. Storage (ctxt → dPKMS)

**Contract:**
- ctxt produces enriched knowledge objects
- dPKMS persists and indexes them
- dPKMS provides query interface

**Flow:**
```
ctxt pipeline: completes enrichment
  ↓
ctxt: creates KnowledgeObject struct
  ↓
dPKMS: WriteKnowledgeObject() → SQLite/Postgres
  ↓
dPKMS: updates FTS index
  ↓
dPKMS: updates entity backlinks
  ↓
ctxt list: queries dPKMS storage → displays results
```

**Interface (dPKMS):**
```go
type Storage interface {
    WriteKnowledgeObject(ctx context.Context, obj KnowledgeObject) error
    Query(ctx context.Context, query Query) ([]KnowledgeObject, error)
}
```

---

### 4. Registries (dPKMS provides, ctxt uses)

**Contract:**
- dPKMS implements registry protocol (HTTP, caching, sync)
- dPKMS stores registry data locally
- ctxt uses registry data for enrichment decisions

**Flow:**
```
ctxt: needs taxonomy for tag validation
  ↓
ctxt: calls dPKMS registry client
  ↓
dPKMS: fetches from cache or remote
  ↓
dPKMS: returns taxonomy
  ↓
ctxt: uses taxonomy in pipeline
```

**Interface (dPKMS):**
```go
type RegistryClient interface {
    FetchTaxonomy(ctx context.Context, url string) (Taxonomy, error)
    FetchEntities(ctx context.Context, url string) ([]Entity, error)
    Sync(ctx context.Context, url string) error
}
```

---

### 5. Search & Query (ctxt → dPKMS)

**Contract:**
- ctxt accepts user query strings
- dPKMS parses RSQL → AST → SQL
- dPKMS executes query and returns results
- ctxt applies reranking and presentation

**Flow:**
```
User: ctxt list --q "tag:ui AND mention:stripe.*"
  ↓
ctxt: sends query string to dPKMS
  ↓
dPKMS: RSQL parser → AST
  ↓
dPKMS: AST → SQL transpiler
  ↓
dPKMS: executes SQL query
  ↓
dPKMS: returns KnowledgeObject[]
  ↓
ctxt: applies reranking (RRF, vector similarity)
  ↓
ctxt: formats and displays results
```

**Interface (dPKMS):**
```go
type QueryEngine interface {
    Parse(ctx context.Context, queryString string) (AST, error)
    Execute(ctx context.Context, ast AST) ([]KnowledgeObject, error)
}
```

**Interface (ctxt):**
```go
type Reranker interface {
    Rerank(ctx context.Context, results []KnowledgeObject, query Query) ([]KnowledgeObject, error)
}
```

---

### 6. Configuration (Shared but scoped)

**Contract:**
- Single config file: `~/.config/contexthelp/config.yaml`
- dPKMS sections: storage, jobs, registries, security
- ctxt sections: pipelines, ai_providers, profiles
- Both packages read their respective sections

**Config Structure:**
```yaml
# dPKMS configuration
storage:
  type: sqlite
  path: ~/.local/share/contexthelp/data.db

jobs:
  workers: 4
  retry_limit: 3

registries:
  - name: uxpatterns
    url: https://registry.example.com

# ctxt configuration
ai_providers:
  openai:
    key: ${OPENAI_API_KEY}

pipelines:
  defaults:
    text: text.long
    url: url.generic

profiles:
  founder:
    registries: [uxpatterns]
    pipelines: [text.long, url.generic]
```

---

## API Endpoint Ownership

### dPKMS REST API (port 7700)

**Endpoints provided by dPKMS:**
- `GET /jobs` - List jobs (dPKMS job queue)
- `GET /jobs/{id}` - Get job details
- `POST /jobs` - Internal: enqueue job
- `GET /knowledge-objects` - Query storage
- `GET /knowledge-objects/{id}` - Get by ID
- `GET /entities` - List entities
- `GET /entities/{slug}` - Get entity
- `GET /entities/{slug}/backlinks` - Get backlinks (graph)
- `POST /registries/sync` - Sync registry

### ctxt CLI (wraps dPKMS APIs)

**Commands that call dPKMS:**
- `ctxt analyze` → `POST /jobs` (via dPKMS library)
- `ctxt list` → `GET /knowledge-objects` (via dPKMS library)
- `ctxt open` → `GET /knowledge-objects/{id}`
- `ctxt jobs` → `GET /jobs`
- `ctxt registry sync` → `POST /registries/sync`

---

## Plugin Integration Points

### dPKMS Plugin Capabilities

Plugins can extend:
- Storage backends
- Queue backends
- Registry providers
- Security/encryption providers

### ctxt Plugin Capabilities

Plugins can extend:
- Pipeline steps
- AI providers
- Reranking algorithms
- Output formatters

### Cross-Package Plugins

Some plugins span both:
- **RSS Feed Plugin:** ctxt defines pipeline, dPKMS manages refresh jobs
- **Notification Plugin:** dPKMS emits events, plugin decides how to notify
- **Price Monitor Plugin:** ctxt extracts prices, plugin defines rules, dPKMS stores historical data

---

## Summary

| Concern | Owner | Used By | Contract |
|---------|-------|---------|----------|
| Job Queue | dPKMS | ctxt enqueues | `JobQueue` interface |
| Pipeline Runtime | dPKMS | executes ctxt definitions | `PipelineRunner` |
| Pipeline Definitions | ctxt | executed by dPKMS | `PipelineDefinition` |
| Storage | dPKMS | ctxt writes/reads | `Storage` interface |
| Entities | dPKMS | ctxt extracts mentions | `EntityStore` |
| Mention Extraction | ctxt | stored by dPKMS | `MentionExtractor` |
| Registries | dPKMS | ctxt uses data | `RegistryClient` |
| Query Engine | dPKMS | ctxt sends queries | `QueryEngine` |
| Reranking | ctxt | uses dPKMS results | `Reranker` |
| CLI Commands | ctxt | calls dPKMS APIs | N/A |
| Worker Daemon | dPKMS | executes jobs | N/A |

This contract ensures clean separation and enables independent evolution of both packages.
