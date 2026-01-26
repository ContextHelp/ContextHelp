# The Shared Kernel (Skeleton 0)

## Package Focus

**Primary Package:** dPKMS (100%)

This skeleton establishes the foundational contracts that enable dPKMS and ctxt to evolve independently. All work in Skeleton 0 defines **interfaces and schemas** that both packages will use, but the implementation is entirely within dPKMS.

**Why dPKMS-focused:** The substrate must exist before the brain can function. Storage, jobs, and entity infrastructure are pure dPKMS concerns.

---

### 1. The Storage Contract (SQL Schema) — dPKMS

**Owner:** dPKMS Infrastructure Team

**Why:** All teams require a stable, guaranteed schema — ingestion must write into it, search must query from it, registries must sync into it, and the graph builder must rely on it.

**Deliverable:** `dpkms/migrations/001_initial_schema.sql`

The storage contract defines the *immutable minimum* required for ContextHelp (dPKMS substrate) to function.
It now includes the semantic identity layer — **entities**, **mentions**, and **graph backlinks** — ensuring every subsystem works against the same foundation.

The `knowledge_objects` table and related tables must define:

- how `tags` are stored (`JSONB`, array, or association table)
- how **mentions** are stored (array of canonical entity IDs, or association table)
- how raw content is captured (`TEXT`, blob, or external pointer)
- how entity references are persisted
  - **entities table** — canonical definitions
  - **entity_backlinks table** — knowledge_object → entity relationships

These tables must also be present at minimum:

- **`entities`** — local or registry-synced canonical entities (id, title, aliases, metadata)
- **`entity_backlinks`** — each mention produces an edge linking knowledge_object → entity

This set of tables defines the **semantic substrate** of dPKMS and cannot be optional for any deployment.

---

### 2. The Domain Model (Go Structs) — dPKMS

**Owner:** dPKMS Infrastructure Team

**Why:** Typed domain structs ensure consistency across ingestion, search, registries, plugins, and the knowledge graph. Teams cannot implement pipelines, storage, or registry logic until these types are stable.

**Deliverable:** `dpkms/pkg/domain/types.go`

Required core types:

```go
type KnowledgeObject struct {
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

### 3. The Interface Definitions (Go Interfaces) — dPKMS

**Owner:** Architecture Lead / dPKMS Team

**Why:** Clear boundaries allow parallel development. Each team can implement or mock the interfaces they depend on. Interfaces must explicitly support mentions, entities, backlinks, and registry-driven resolution.

**Deliverable:** `dpkms/pkg/ports/interfaces.go`

Example boundaries:

```go
// PipelineRunner enqueues ingestion jobs (dPKMS runtime).
type PipelineRunner interface {
    Enqueue(ctx context.Context, job domain.Job) (string, error)
}

// Storage backend handles knowledge_object + entity persistence (dPKMS).
type Storage interface {
    WriteKnowledgeObject(ctx context.Context, obj domain.KnowledgeObject) error
    GetPendingJobs(ctx context.Context) ([]domain.Job, error)

    // Semantic identity layer (dPKMS graph)
    UpsertEntity(ctx context.Context, e domain.Entity) error
    AddBacklink(ctx context.Context, entityID, objectID string) error

    // Mention/entity queries (dPKMS query engine)
    GetEntity(ctx context.Context, id string) (domain.Entity, error)
}

// RegistryClient provides taxonomies + canonical entities (dPKMS federation).
type RegistryClient interface {
    FetchTaxonomy(url string) (domain.Taxonomy, error)
    FetchEntities(url string) ([]domain.Entity, error)

    // Alias / namespace lookup
    ResolveEntity(id string) (domain.Entity, error)
}
```

These interfaces enable:

- **ctxt pipelines** to perform mention extraction + entity resolution
- **dPKMS storage** layer to maintain a persistent semantic graph
- **dPKMS registries** to supply canonical metadata, aliases, and translations
- **dPKMS search** to query entities and mentions using a stable storage contract

They are the interoperability layer across the entire ContextHelp platform.

---

### The Workflow Result

Completing **Skeleton 0** establishes the foundation for all future work:

- **ctxt Ingestion Team**
  Uses `Storage` + `RegistryClient` interfaces (from dPKMS) to implement pipeline definitions, mention extraction logic, and entity resolution workflows. Can test against mocks with no DB running.

- **dPKMS Search Team**
  Builds mention-aware filtering, entity-aware queries, graph-powered lookups, and AST translation using the finalized schema.

- **dPKMS Registry Team**
  Implements taxonomy and entity syncing, alias resolution, and registry merging using stable interfaces.

- **ctxt CLI Team**
  Builds `ctxt analyze`, `ctxt list`, `ctxt search` commands that leverage dPKMS capabilities (mention filters, entity lookups, graph-based retrieval).

- **dPKMS Infrastructure Team**
  Provides `dpkms serve` daemon and underlying infrastructure guarantees.

The Shared Kernel ensures all teams work on top of **the same semantic and structural guarantees**, enabling ContextHelp to evolve cleanly without cross-team blocking.
---

## See Also

**Package Boundaries:**
- [CROSS-PACKAGE-CONTRACTS.md](CROSS-PACKAGE-CONTRACTS.md) - dPKMS ↔ ctxt integration points
- [../branding.md](../branding.md) - Naming conventions (dPKMS vs ctxt vs ContextHelp)
- [../dpkms-or-ctxt.md](../dpkms-or-ctxt.md) - Package placement guide

**Configuration:**
- [CONFIGURATION-STRUCTURE.md](CONFIGURATION-STRUCTURE.md) - Config file organization
- [../ctxt/configuration.md](../ctxt/configuration.md) - Focus profiles & preferences

**Architecture:**
- [../architecture.md](../architecture.md) - System architecture overview
- [../../ROADMAP.md](../../ROADMAP.md) - Living skeleton roadmap
- [README.md](README.md) - Sprint documentation index
