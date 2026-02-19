# ADR-059 – Runtime Pipeline Mutation with Revisions

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A
> **References:** ADR-004, ADR-053, ADR-057

---

## Context

ctxt's pipeline system (ADR-004) currently supports:
- Step-based pipelines with idempotent, retry-safe steps
- Archive/unarchive for lifecycle management
- Simple `updated_at` timestamp for tracking changes

**Problem:** Pipelines are static at runtime. To modify a pipeline:
1. Edit the config file or use `dpkms pipeline create`
2. Pipeline definition is overwritten (no history)
3. Running jobs use whatever definition was loaded at start
4. No way to audit what changed, when, or by whom
5. No rollback capability

**Design goal:** Pipeline configurations should be versioned entities
- Changes are tracked with full provenance
- In-memory runtime can be hot-updated
- Changes are reversible with audit trail

---

## Decision

**Implement versioned pipelines with runtime mutation support.**

### Core Model

```
┌─────────────────────────────────────────────────────────────────┐
│                    PIPELINE VERSION MODEL                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  pipelines (current view)          pipeline_revisions (history) │
│  ┌─────────────────────┐          ┌─────────────────────────┐   │
│  │ id                  │          │ id                      │   │
│  │ name                │◄─────────│ pipeline_id             │   │
│  │ current_revision    │          │ revision_number         │   │
│  │ description         │          │ steps (JSON)            │   │
│  │ steps (JSON)        │          │ sandbox (JSON)          │   │
│  │ sandbox (JSON)      │          │ change_reason           │   │
│  │ is_built_in         │          │ changed_by              │   │
│  │ archived            │          │ changed_at              │   │
│  │ created_at          │          │ parent_revision_id ─────┼───┤
│  │ updated_at          │          │ checksum                │   │
│  └─────────────────────┘          └─────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Key Components

#### 1. PipelineRevision Entity

```go
type PipelineRevision struct {
    ID              string         `json:"id"`
    PipelineID      string         `json:"pipeline_id"`
    RevisionNumber  int            `json:"revision_number"`  // Monotonic, starts at 1
    Steps           []StepRef      `json:"steps"`
    Sandbox         *SandboxConfig `json:"sandbox,omitempty"`
    Description     string         `json:"description"`      // Snapshot of pipeline desc
    ChangeReason    string         `json:"change_reason"`    // Human-readable why
    ChangedBy       string         `json:"changed_by"`       // User/system identifier
    ParentRevisionID *string       `json:"parent_revision_id,omitempty"`
    Checksum        string         `json:"checksum"`         // Content hash for dedup
    CreatedAt       time.Time      `json:"created_at"`
}
```

#### 2. Updated Pipeline Entity

```go
type Pipeline struct {
    ID              string         `json:"id"`
    Name            string         `json:"name"`
    Description     string         `json:"description"`
    Steps           []StepRef      `json:"steps"`             // Current version
    IsBuiltIn       bool           `json:"is_built_in"`
    Archived        bool           `json:"archived"`
    Sandbox         *SandboxConfig `json:"sandbox,omitempty"`
    CurrentRevision int            `json:"current_revision"`  // NEW: points to active version
    CreatedAt       time.Time      `json:"created_at"`
    UpdatedAt       time.Time      `json:"updated_at"`
}
```

#### 3. Runtime Mutation Protocol

```go
type PipelineMutationAPI interface {
    // Update creates a new revision and updates runtime
    UpdatePipeline(ctx context.Context, name string, req MutationRequest) (*Pipeline, error)
    
    // Rollback restores a previous revision
    RollbackPipeline(ctx context.Context, name string, toRevision int) (*Pipeline, error)
    
    // GetRevision retrieves a specific historical version
    GetRevision(ctx context.Context, name string, revision int) (*PipelineRevision, error)
    
    // ListRevisions shows change history
    ListRevisions(ctx context.Context, name string, opts RevisionListOpts) ([]PipelineRevision, error)
    
    // DiffRevisions compares two versions
    DiffRevisions(ctx context.Context, name string, from, to int) (*RevisionDiff, error)
}

type MutationRequest struct {
    Steps        *[]StepRef      `json:"steps,omitempty"`        // nil = no change
    Sandbox      *SandboxConfig  `json:"sandbox,omitempty"`      // nil = no change
    Description  *string         `json:"description,omitempty"`  // nil = no change
    Reason       string          `json:"reason"`                 // Required: why this change
    ChangedBy    string          `json:"changed_by"`             // User or system ID
}
```

### Runtime Behavior

#### Hot-Update Strategy

```
┌─────────────────────────────────────────────────────────────────┐
│                    RUNTIME PIPELINE REGISTRY                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   ┌──────────────┐     ┌──────────────┐     ┌──────────────┐    │
│   │  Pipeline    │     │  Pipeline    │     │  Pipeline    │    │
│   │  "text.short"│     │  "text.long" │     │  "image.ocr" │    │
│   │  v3 ◄───────┼─────┼── v2 ◄───────┼─────┼── v1         │    │
│   └──────────────┘     └──────────────┘     └──────────────┘    │
│          │                    │                    │             │
│          ▼                    ▼                    ▼             │
│   ┌──────────────┐     ┌──────────────┐     ┌──────────────┐    │
│   │ Step[] Cache │     │ Step[] Cache │     │ Step[] Cache │    │
│   │ (compiled)   │     │ (compiled)   │     │ (compiled)   │    │
│   └──────────────┘     └──────────────┘     └──────────────┘    │
│                                                                  │
│   Mutation Flow:                                                 │
│   1. Validate new revision                                       │
│   2. Begin transaction                                           │
│   3. Write new revision to pipeline_revisions                    │
│   4. Update pipelines.current_revision                           │
│   5. Invalidate cache entry                                      │
│   6. Lazy recompile on next access                               │
│   7. Commit transaction                                          │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### In-Flight Job Handling

When a pipeline mutates while jobs are running:

1. **Job captures revision at enqueue time:**
   ```go
   type Job struct {
       // ... existing fields ...
       PipelineRevision int `json:"pipeline_revision"` // Snapshot at enqueue
   }
   ```

2. **Worker loads specific revision:**
   ```go
   func (w *Worker) processJob(ctx context.Context, job *Job) error {
       // Load the exact revision this job was enqueued with
       pipeline, err := w.registry.GetRevision(job.Pipeline, job.PipelineRevision)
       // ... execute with that version
   }
   ```

3. **Old revisions retained for N days (configurable):**
   - Default: 30 days retention
   - Built-in pipelines: retain all revisions
   - Custom pipelines: configurable retention policy

---

## Rationale

### Why Versioned Mutations?

| Need | Without This ADR | With This ADR |
|------|------------------|---------------|
| Audit who changed what | Only `updated_at` | Full change log with `changed_by`, `reason` |
| Rollback bad change | Manual restore from backup | `dpkms pipeline rollback <name> <rev>` |
| Debug job failures | "What was the pipeline then?" | Exact revision stored on job |
| Compliance/traceability | No history | Immutable revision chain |

### Why Not Event Sourcing?

Event sourcing was considered (store `PipelineCreated`, `StepsChanged`, `StepAdded` events):

**Rejected because:**
- Overkill for configuration entities (vs transactional domain events)
- Complex to query "what was the pipeline at time T"
- Snapshot pattern would be needed anyway for runtime
- Simpler to store revisions directly (like git commits)

### Why Lazy Recompile vs Eager?

When pipeline changes, we could:
1. **Eager:** Immediately recompile all step instances
2. **Lazy:** Invalidate cache, recompile on next access

**Chose lazy because:**
- Minimizes latency of mutation API
- Pipeline may be updated but not immediately used
- Natural fit for Go's lazy initialization patterns
- Compiles are fast (step instantiation is just config parsing)

---

## Implementation Phases

### Phase 1: Storage Layer (Week 1)

1. **Migration: Add `pipeline_revisions` table**
   ```sql
   CREATE TABLE pipeline_revisions (
       id TEXT PRIMARY KEY,
       pipeline_id TEXT NOT NULL,
       revision_number INTEGER NOT NULL,
       steps TEXT NOT NULL,  -- JSON
       sandbox TEXT,         -- JSON
       description TEXT,
       change_reason TEXT NOT NULL,
       changed_by TEXT NOT NULL,
       parent_revision_id TEXT,
       checksum TEXT NOT NULL,
       created_at TEXT NOT NULL,
       FOREIGN KEY (pipeline_id) REFERENCES pipelines(id),
       UNIQUE(pipeline_id, revision_number)
   );
   ```

2. **Migration: Add `current_revision` to `pipelines`**

3. **Update PipelineStore with revision methods**

4. **Backfill existing pipelines as revision 1**

### Phase 2: API Layer (Week 1-2)

1. **Extend `dpkms pipeline update` with `--reason` flag**
   ```bash
   dpkms pipeline update text.long --reason "Added sentiment step for better analysis" \
       --step-add sentiment-analyzer --step-after tag-extractor
   ```

2. **Add `dpkms pipeline history` command**
   ```bash
   dpkms pipeline history text.long
   # REV  CHANGED_AT           CHANGED_BY   REASON
   # 3    2026-02-18 14:30    alice        Added sentiment step
   # 2    2026-02-15 09:00    system       Registry auto-update
   # 1    2026-02-01 10:00    admin        Initial creation
   ```

3. **Add `dpkms pipeline rollback` command**
   ```bash
   dpkms pipeline rollback text.long 2 --reason "Sentiment step causing errors"
   ```

4. **Add `dpkms pipeline diff` command**
   ```bash
   dpkms pipeline diff text.long 2 3
   # Steps changed:
   #   + sentiment-analyzer (after tag-extractor)
   ```

### Phase 3: Runtime Integration (Week 2)

1. **Update Job to capture revision**
2. **Update Worker to load specific revision**
3. **Update Registry to support hot-reload**
4. **Add revision GC job (cleanup old revisions)**

### Phase 4: Observability (Week 3)

1. **Emit metrics on pipeline mutations**
   - `dpkms_pipeline_revisions_total{pipeline="text.long"}`
   - `dpkms_pipeline_rollbacks_total{pipeline="text.long"}`

2. **Structured logging for mutations**
   ```json
   {"level":"info","msg":"pipeline updated","pipeline":"text.long",
    "from_rev":2,"to_rev":3,"changed_by":"alice","reason":"..."}
   ```

3. **CLI audit trail export**
   ```bash
   dpkms pipeline audit --format json --since 2026-01-01
   ```

---

## Consequences

### Positive

- **Full audit trail** for compliance and debugging
- **Safe rollback** from bad changes
- **Deterministic job execution** (jobs use revision at enqueue time)
- **Hot updates** without service restart
- **Diff/compare** capability for change review

### Negative

- **Storage overhead** for revision history (mitigated by GC)
- **API complexity** (new endpoints, flags)
- **Job schema change** (migration for existing jobs)
- **Mental model** shift for operators (pipelines are now versioned)

### Neutral

- Revision numbers are scoped per-pipeline (not global)
- Built-in pipelines still follow same versioning (but initial revision is immutable)
- Archive hides from list but revisions remain queryable

---

## Open Questions

1. **Revision pruning strategy:** Should we auto-prune revisions older than N days? What about rollback depth limits?

2. **Step version tracking:** If a step's implementation changes (new version installed), should that create a pipeline revision automatically? (Leaning: yes, via ADR-057 notification flow)

3. **Concurrent mutation handling:** Multiple admins updating same pipeline simultaneously - use optimistic locking or last-writer-wins? (Leaning: optimistic with revision number in request)

4. **Import/export:** How do revisions interact with ADR-020 export/import portability?

---

## References

- **ADR-004** – Step-Based Pipeline Architecture
- **ADR-053** – KnowledgeObject as Pipeline Draft
- **ADR-057** – Registry Update Notification Model
- **ADR-038** – SuperMemory versioning patterns (not adopted, but `isLatest/parentMemoryId` concepts relevant)

---
