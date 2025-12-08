# The Shared Kernel

### 1. The Storage Contract (SQL Schema)

**Owner:** Core Team + Search Team

**Why:** All teams require a stable, guaranteed schema — ingestion must write into it, search must query from it, registries must sync into it, and the graph builder must rely on it.

**Deliverable:** `migrations/001_initial_schema.sql`

The storage contract defines the *immutable minimum* required for ContextHelp to function.
It now includes the semantic identity layer — **entities**, **mentions**, and **graph backlinks** — ensuring every subsystem works against the same foundation.

The `bookmarks` table and related tables must define:

- how `tags` are stored (`JSONB`, array, or association table)
- how **mentions** are stored (array of canonical entity IDs, or association table)
- how raw content is captured (`TEXT`, blob, or external pointer)
- how entity references are persisted
  - **entities table** — canonical definitions
  - **entity_backlinks table** — bookmark → entity relationships

These tables must also be present at minimum:

- **`entities`** — local or registry-synced canonical entities (id, title, aliases, metadata)
- **`entity_backlinks`** — each mention produces an edge linking bookmark → entity

This set of tables defines the **semantic substrate** of the system and cannot be optional for any deployment.

---

### 2. The Domain Model (Go Structs)

**Owner:** Core Team

**Why:** Typed domain structs ensure consistency across ingestion, search, registries, plugins, and the knowledge graph. Teams cannot implement pipelines, storage, or registry logic until these types are stable.

**Deliverable:** `pkg/domain/types.go`

Required core types:

```go
type Bookmark struct {
    ID         string
    Raw        string
    Tags       []Tag
    Mentions   []string // canonical entity IDs extracted from content
    // other metadata, sections, provenance, etc.
}

type Entity struct {
    ID          string
    Title       string
    Description string
    Aliases     []string
    // registry provenance, metadata, translations
}

type Job struct {
    ID        string
    Type      string
    Input     string
    State     string
    CreatedAt time.Time
    UpdatedAt time.Time
    // and any pipeline-specific fields
}

type Tag struct {
    Label     string
    Polarity  int
    Weight    float64
    // provenance, registry source, metadata
}
```

These structs define the canonical data model shared across ingestion, search, registry syncing, plugins, and graph updates.

Once this file is finalized, all teams can work independently using mocks.

---

### 3. The Interface Definitions (Go Interfaces)

**Owner:** Architecture Lead / All Leads

**Why:** Clear boundaries allow parallel development. Each team can implement or mock the interfaces they depend on. Interfaces must explicitly support mentions, entities, backlinks, and registry-driven resolution.

**Deliverable:** `pkg/ports/interfaces.go`

Example boundaries:

```go
// Pipelines enqueue ingestion jobs.
type PipelineRunner interface {
    Enqueue(ctx context.Context, job domain.Job) (string, error)
}

// Storage backend handles bookmark + entity persistence.
type Storage interface {
    WriteBookmark(ctx context.Context, b domain.Bookmark) error
    GetPendingJobs(ctx context.Context) ([]domain.Job, error)

    // Semantic identity layer
    UpsertEntity(ctx context.Context, e domain.Entity) error
    AddBacklink(ctx context.Context, entityID, bookmarkID string) error

    // Mention/entity queries may use this later
    GetEntity(ctx context.Context, id string) (domain.Entity, error)
}

// Registries provide taxonomies + canonical entities.
type RegistryClient interface {
    FetchTaxonomy(url string) (domain.Taxonomy, error)
    FetchEntities(url string) ([]domain.Entity, error)

    // Alias / namespace lookup
    ResolveEntity(id string) (domain.Entity, error)
}
```

These interfaces enable:

- ingestion pipelines to perform mention extraction + entity resolution
- storage layer to maintain a persistent semantic graph
- registries to supply canonical metadata, aliases, and translations
- search to query entities and mentions using a stable storage contract

They are the interoperability layer across the entire platform.

---

### The Workflow Result

Completing **Sprint 0** establishes the foundation for all future work:

- **Ingestion Team**
  Uses `Storage` + `RegistryClient` interfaces to implement pipelines, mention extraction, and entity resolution. Can test against mocks with no DB running.

- **Search Team**
  Builds mention-aware filtering, entity-aware queries, graph-powered lookups, and AST translation using the finalized schema.

- **Registry Team**
  Implements taxonomy and entity syncing, alias resolution, and registry merging using stable interfaces.

- **Core Team**
  Extends CLI, REST, and gRPC with mention filters, entity lookups, and graph-based retrieval, confident the shared kernel guarantees consistency.

The Shared Kernel ensures all teams work on top of **the same semantic and structural guarantees**, enabling the platform to evolve cleanly without cross-team blocking.