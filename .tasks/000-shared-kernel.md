# Skeleton 0: Shared Kernel Tasks

**Package Focus:** dPKMS (100%)

**Goal:** Establish foundational contracts that enable dPKMS and ctxt to evolve independently.

---

## dPKMS Infrastructure Team

### Storage Contract (SQL Schema)

- [PLAN] 📦 *dPKMS* Create `dpkms/migrations/001_initial_schema.sql`
  - [ ] Define `knowledge_objects` table with:
    - [ ] `id` (UUID, primary key)
    - [ ] `raw` (TEXT, original content)
    - [ ] `tags` (JSONB array or association table)
    - [ ] `mentions` (JSONB array of canonical entity IDs) **required, not optional**
    - [ ] `created_at`, `updated_at` timestamps
    - [ ] provenance metadata fields
  - [ ] Define `entities` table (semantic substrate) **required minimum**:
    - [ ] `id` (TEXT, primary key, canonical namespace.slug)
    - [ ] `title` (TEXT)
    - [ ] `description` (TEXT)
    - [ ] `aliases` (JSONB array)
    - [ ] `namespace` (TEXT, indexed)
    - [ ] `version` (INTEGER)
    - [ ] `registry_source` (TEXT, provenance)
    - [ ] `metadata` (JSONB, extensible)
  - [ ] Define `entity_backlinks` table (knowledge graph) **required minimum**:
    - [ ] `entity_id` (TEXT, references entities.id)
    - [ ] `knowledge_object_id` (TEXT, references knowledge_objects.id)
    - [ ] `created_at` (TIMESTAMP)
    - [ ] Composite primary key (entity_id, knowledge_object_id)
    - [ ] Indexes for both directions of traversal
  - [ ] Define `jobs` table:
    - [ ] `id`, `type`, `input`, `state`, `attempts`
    - [ ] `created_at`, `updated_at`, `completed_at`
  - [ ] Define `job_steps` table for tracing:
    - [ ] `job_id`, `step_number`, `name`, `status`, `output`
  - [ ] Document schema invariants and constraints

### Domain Model (Go Structs)

- [PLAN] 📦 *dPKMS* Create `dpkms/pkg/domain/types.go`
  - [ ] Define `KnowledgeObject` struct:
    ```go
    type KnowledgeObject struct {
        ID         string
        Raw        string
        Tags       []Tag
        Mentions   []string // canonical entity IDs
        Sections   []Section
        Provenance Provenance
        CreatedAt  time.Time
        UpdatedAt  time.Time
    }
    ```
  - [ ] Define `Entity` struct:
    ```go
    type Entity struct {
        ID              string   // namespace.slug
        Title           string
        Description     string
        Aliases         []string
        Namespace       string
        Version         int
        RegistrySource  string
        Metadata        map[string]interface{}
    }
    ```
  - [ ] Define `Job` struct:
    ```go
    type Job struct {
        ID        string
        Type      string
        Input     string
        State     string // Pending, Running, Completed, Failed
        Attempts  int
        CreatedAt time.Time
        UpdatedAt time.Time
    }
    ```
  - [ ] Define `Tag` struct:
    ```go
    type Tag struct {
        Label     string
        Polarity  int     // -1, 0, 1
        Weight    float64
        Provenance string
    }
    ```
  - [ ] Define supporting types: `Section`, `Provenance`, `JobStep`
  - [ ] Add JSON serialization tags
  - [ ] Document field semantics and invariants

### Interface Definitions (Go Interfaces)

- [PLAN] 📦 *dPKMS* Create `dpkms/pkg/ports/interfaces.go`
  - [ ] Define `PipelineRunner` interface:
    ```go
    type PipelineRunner interface {
        Enqueue(ctx context.Context, job domain.Job) (string, error)
        GetJob(ctx context.Context, id string) (domain.Job, error)
    }
    ```
  - [ ] Define `Storage` interface with semantic identity layer:
    ```go
    type Storage interface {
        // Knowledge Objects
        WriteKnowledgeObject(ctx context.Context, obj domain.KnowledgeObject) error
        GetKnowledgeObject(ctx context.Context, id string) (domain.KnowledgeObject, error)
        ListKnowledgeObjects(ctx context.Context, filters Filters) ([]domain.KnowledgeObject, error)

        // Jobs
        EnqueueJob(ctx context.Context, job domain.Job) (string, error)
        AcquireNextJob(ctx context.Context) (*domain.Job, error)
        UpdateJobStatus(ctx context.Context, id string, status string) error
        GetPendingJobs(ctx context.Context) ([]domain.Job, error)

        // Semantic Identity Layer (dPKMS graph)
        UpsertEntity(ctx context.Context, e domain.Entity) error
        GetEntity(ctx context.Context, id string) (domain.Entity, error)
        ListEntities(ctx context.Context, namespace string) ([]domain.Entity, error)
        AddBacklink(ctx context.Context, entityID, objectID string) error
        GetBacklinks(ctx context.Context, entityID string) ([]string, error)
    }
    ```
  - [ ] Define `RegistryClient` interface:
    ```go
    type RegistryClient interface {
        FetchTaxonomy(url string) (domain.Taxonomy, error)
        FetchEntities(url string) ([]domain.Entity, error)
        ResolveEntity(id string) (domain.Entity, error)
        GetAliases(entityID string) ([]string, error)
    }
    ```
  - [ ] Document interface contracts and threading guarantees
  - [ ] Add mock implementations for testing

---

## Validation Criteria

- [ ] All three files (schema, types, interfaces) reviewed and approved
- [ ] Entity, mention, and backlink tables present in schema
- [ ] Cross-team dependencies clearly documented
- [ ] Mock implementations allow parallel development
- [ ] No team blocked waiting for others

---

## See Also

- Sprint spec: `docs/sprints/000-shared-kernel.md`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- Package placement: `docs/dpkms-or-ctxt.md`
