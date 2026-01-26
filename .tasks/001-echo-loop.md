# Skeleton 1: Echo Loop Tasks

**Package Focus:** dPKMS (70%) + ctxt (30%)

**Goal:** Prove end-to-end execution with minimal features. First integration of dPKMS substrate with ctxt brain.

---

## dPKMS Infrastructure Team (The Backbone)

### Bootloader & Schema Implementation

- [PLAN] 📦 *dPKMS* Initialize `dpkms/cmd/dpkms/main.go`
  - [ ] Load config from `~/.config/contexthelp/config.yaml`
  - [ ] Connect to SQLite database
  - [ ] Enable WAL mode (critical for concurrency)
  - [ ] Run schema migrations from Skeleton 0
  - [ ] **Verify `mentions` column exists in knowledge_objects table (empty by default)**
  - [ ] Initialize logging subsystem
  - [ ] Handle graceful shutdown

### Job Queue Implementation

- [PLAN] 📦 *dPKMS* Implement JobStore in `dpkms/pkg/storage/`
  - [ ] Implement `EnqueueJob()`:
    - [ ] SQL INSERT with transaction
    - [ ] Set initial status to `Pending`
    - [ ] Return job ID
  - [ ] Implement `AcquireNextJob()`:
    - [ ] Use `SELECT ... FOR UPDATE` or SQLite locking
    - [ ] Atomic status transition: `Pending` → `Running`
    - [ ] Respect job priority if defined
  - [ ] Implement `UpdateJobStatus()`:
    - [ ] Support transitions: `Running` → `Completed` / `Failed`
    - [ ] Update timestamps
    - [ ] Store error messages on failure
  - [ ] Add indexes for job queue performance

### Worker Service

- [PLAN] 📦 *dPKMS* Create worker in `dpkms/pkg/worker/`
  - [ ] Background polling loop calling `AcquireNextJob()`
  - [ ] Execute pipeline steps (call ctxt-defined pipelines)
  - [ ] Handle graceful shutdown (don't kill jobs mid-process)
  - [ ] Worker respects cancellation context
  - [ ] Log job execution to `job_steps` table

---

## ctxt Ingestion Team (The Writer)

### Echo Pipeline Definition

- [PLAN] 📦 *ctxt* Create `text.echo` pipeline in `ctxt/pkg/pipelines/`
  - [ ] Define pipeline interface:
    ```go
    type Pipeline interface {
        Execute(ctx context.Context, input string) (domain.KnowledgeObject, error)
    }
    ```
  - [ ] Implement `text.echo`:
    - [ ] Accept input text
    - [ ] Sleep 1 second (simulate processing)
    - [ ] Create KnowledgeObject with title "Processed: {input}"
    - [ ] **Explicitly set `mentions: []` in output to establish contract**
    - [ ] Return structured result
  - [ ] Register pipeline in pipeline registry

### Knowledge Object Writer

- [PLAN] 📦 *dPKMS* Implement persistence layer
  - [ ] Accept KnowledgeObject from pipeline
  - [ ] Write to `knowledge_objects` table
  - [ ] **Always persist `mentions: []` even if empty**
  - [ ] Verify round-trip of mentions field
  - [ ] Handle database errors gracefully

---

## dPKMS Search Team (The Reader)

### Basic Read Operations

- [PLAN] 📦 *dPKMS* Implement `ListKnowledgeObjects()` in storage layer
  - [ ] Basic SQL SELECT from knowledge_objects
  - [ ] Map rows to `KnowledgeObject` structs
  - [ ] **Deserialize `mentions` array correctly**
  - [ ] Support basic filtering (by date, status)
  - [ ] Return paginated results

### CLI List Command

- [ ] 📦 *ctxt* Implement `ctxt list` command in `ctxt/cmd/ctxt/`
  - [ ] Call dPKMS storage via interface
  - [ ] Print table of results (ID, title, created_at)
  - [ ] **Optionally show "mentions: n" column to validate wiring**
  - [ ] Handle empty results gracefully

### CLI Open Command

- [ ] 📦 *ctxt* Implement `ctxt open <id>` command
  - [ ] Retrieve single knowledge object by ID from dPKMS
  - [ ] Output as formatted JSON
  - [ ] **Confirm `mentions: []` is present and correct**
  - [ ] Handle not-found errors

### CLI Jobs Command

- [ ] 📦 *ctxt* Implement `ctxt jobs list` command
  - [ ] Query job queue status from dPKMS
  - [ ] Display table: ID, type, status, created_at
  - [ ] Show running/pending/completed counts
  - [ ] Enable debugging of pipeline execution

---

## dPKMS Registry Team (The Connector)

### Local Registry Loader

- [PLAN] 📦 *dPKMS* Create `dpkms/pkg/registry/` module
  - [ ] Implement loading `taxonomy.json` from disk
  - [ ] Validate JSON schema
  - [ ] Parse taxonomy structure
  - [ ] **Prepare module structure for future `entities.json` (don't implement yet)**
  - [ ] Cache loaded registries in memory

### Tag Enrichment Stub

- [ ] 📦 *ctxt/dPKMS integration* Create `EnrichTags(tags []string)` function
  - [ ] Accept tag list from pipeline
  - [ ] Validate tags exist in local taxonomy (from dPKMS registry)
  - [ ] Return enriched tag metadata
  - [ ] Provide this to ctxt Ingestion Team for Echo Pipeline integration

---

## Integration Check (End-to-End Demo)

### Setup

- [ ] Build `dpkms` binary from dPKMS package
- [ ] Build `ctxt` binary from ctxt package
- [ ] Initialize config and database

### Execution Flow

1. [ ] Run `ctxt analyze --text "Skeleton 1 Test"`
   - [ ] Verify job enqueued in dPKMS job queue
   - [ ] Check job appears in `ctxt jobs list`

2. [ ] Run `dpkms serve`
   - [ ] Worker starts and polls for jobs
   - [ ] Worker acquires Job #1
   - [ ] Worker executes ctxt-defined `text.echo` pipeline
   - [ ] Pipeline calls dPKMS registry to validate fake tag
   - [ ] Worker writes KnowledgeObject to DB **with `mentions: []`**
   - [ ] Job status transitions to `Completed`

3. [ ] Run `ctxt list`
   - [ ] KnowledgeObject appears in results
   - [ ] Mentions column shows 0 or []

4. [ ] Run `ctxt open <id>`
   - [ ] JSON output includes `"mentions": []`
   - [ ] Verify field round-tripped correctly

### Validation Checklist

**dPKMS Validation:**
- [ ] SQLite WAL mode enabled
- [ ] Job queue state transitions work (Pending → Running → Completed)
- [ ] Worker polls and acquires jobs correctly
- [ ] Knowledge object storage persists with `mentions: []` field
- [ ] Entity backlinks table exists (even if empty)
- [ ] Job step tracing captures pipeline execution

**ctxt Validation:**
- [ ] `ctxt analyze` successfully enqueues jobs to dPKMS
- [ ] Echo pipeline definition executes via dPKMS runtime
- [ ] `ctxt list` retrieves results from dPKMS storage
- [ ] `ctxt open` displays structured JSON output
- [ ] `ctxt jobs list` shows job status
- [ ] Pipeline output includes `mentions: []` contract

**Cross-Package Validation:**
- [ ] ctxt → dPKMS job enqueueing works
- [ ] dPKMS executes ctxt-defined pipeline steps
- [ ] ctxt reads from dPKMS storage
- [ ] Configuration loaded correctly by both binaries

---

## Risks to Watch For

- **Database Locking:** WAL mode must be enabled in dPKMS to avoid locking issues
- **Config Path Hell:** Agree on defaults early (`~/.config/contexthelp/`)
- **Schema Drift:** `mentions` field must be added now to avoid painful migrations later
- **Pipeline Contract Drift:** Including `mentions: []` today prevents backward compatibility breaks
- **Package Boundary Confusion:** Clear interfaces between ctxt (pipeline definitions) and dPKMS (pipeline execution)

---

## See Also

- Sprint spec: `docs/sprints/001-echo-loop.md`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- Package placement: `docs/dpkms-or-ctxt.md`
