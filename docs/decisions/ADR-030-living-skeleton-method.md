# ADR-030 – Living Skeleton Development Method

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS + ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

Building a knowledge management system with ambitious scope (local-first, federated, multimodal, agentic, plugin-extensible) creates risk of:

**Development Risks:**
- Big-bang approach delays working software for months
- Feature creep before core spine is validated
- Architectural decisions made in isolation, not validated by real usage
- Late discovery of integration issues
- Rewrites required when reality conflicts with design
- No feedback loop until "v1.0" is complete

**User Impact:**
- No usable system until everything is done
- Cannot validate real-world workflows early
- Switching costs high (all-or-nothing adoption)
- Feedback comes too late to influence architecture

**Project Health:**
- Hard to measure progress (0% until 100%)
- Motivation declines without working software
- Contributors unclear on priorities
- Documentation diverges from implementation

The development methodology must enable:
- **Early validation:** Working software from day one
- **Incremental value:** Each phase adds capability without breaking previous work
- **Architectural stability:** Core contracts remain stable as features expand
- **Feedback integration:** Real usage informs next phases
- **Contributor clarity:** Clear priorities and stable foundation

---

## Decision Drivers

- **Ship Early:** Working software validates architecture decisions
- **Incremental Value:** Each skeleton phase adds usable capability
- **Contract Stability:** Interfaces defined early, implementations upgraded
- **No Rewrites:** Replacement over rewrites (swap implementations, keep contracts)
- **Determinism First:** Correctness and sovereignty before features
- **Feedback Loop:** Real usage informs priorities

---

## Considered Options

### Option 1: Traditional Waterfall

**Approach:**
- Complete design → implement all features → test → ship v1.0
- Single release after everything is done

**Pros:**
- Clear upfront plan
- All features shipped together
- Less coordination overhead

**Cons:**
- No working software for months
- Late validation of architecture
- High rewrite risk
- No user feedback until too late
- Hard to measure progress

### Option 2: Feature-Based Incremental Development

**Approach:**
- Ship features one at a time: storage → pipelines → search → graph → etc.
- Each feature fully implemented before next

**Pros:**
- Incremental delivery
- Focus on one thing at a time

**Cons:**
- No working end-to-end path until many features done
- Integration happens late
- Features may not integrate well
- Users cannot validate workflows until late

### Option 3: Living Skeleton (Vertical Slices)

**Approach:**
- Build minimal end-to-end path first (Skeleton 0)
- Each skeleton phase strengthens the same spine
- Replace placeholders with real implementations
- Maintain working system throughout
- Contracts stabilize early, implementations evolve

**Pros:**
- Working software from day one
- Continuous validation of architecture
- Users can provide feedback early
- Incremental value delivery
- Contracts stabilize early
- No big-bang integration
- Clear progress measurement

**Cons:**
- Requires discipline (resist feature creep)
- Placeholders must be replaced systematically
- Early code may look "incomplete"

---

## Decision Outcome

**Chosen Option:** Option 3 — Living Skeleton (Vertical Slices)

**Rationale:**

This approach aligns with project goals and architectural principles:

- **Ship Early:** Skeleton 0 is a working system (capture → store → retrieve)
- **Incremental:** Each skeleton adds capability without breaking previous work
- **Contract Stability:** Interfaces defined early, implementations swapped
- **Feedback Loop:** Real usage validates design decisions
- **Contributor Clarity:** Each skeleton has clear goals and scope
- **Local-First Principle:** Core contracts validated before federation complexity

This method has been successfully used in:
- Alistair Cockburn's "Walking Skeleton" pattern
- Agile/XP vertical slice development
- Infrastructure projects (Kubernetes, Git)

---

## Implementation Details

### Living Skeleton Principles

**Core Tenets:**

1. **Working End-to-End Path:** Every skeleton phase maintains a complete working system
2. **Replacement Over Rewrites:** Swap implementations, keep contracts
3. **Contract Stability:** Interfaces stabilize early, internals evolve
4. **Correctness Before Features:** Determinism, sovereignty, safety first
5. **Incremental Value:** Each phase adds real capability users can adopt
6. **No Placeholders Forever:** Placeholders explicitly marked for replacement

**Walking Skeleton Definition:**

> "A Walking Skeleton is a tiny implementation of the system that performs a small end-to-end function. It need not use the final architecture, but it should link together the main architectural components. The architecture and the functionality can then evolve in parallel."
> — Alistair Cockburn

### Skeleton Progression Rules

**Phase Structure:**

Each skeleton phase:

1. **Goal:** Clear, measurable objective (what becomes possible?)
2. **Contracts:** Define interfaces that will remain stable
3. **Placeholders:** Identify simplifications (to be replaced later)
4. **Success Criteria:** How do we know this skeleton works?
5. **Dependencies:** What must be completed first?

**Replacement Strategy:**

```
Placeholder → Real Implementation
- Interface stays the same
- Internals upgraded
- Existing features keep working
```

**Example:**

```
Skeleton 0: SQLite in-memory (placeholder)
Skeleton 1: SQLite with WAL on disk (replacement)
Skeleton 10: Optional Postgres backend (extension)

Contract remains stable:
  type StorageBackend interface {
      Store(obj Object) error
      Retrieve(id string) (Object, error)
      Query(filter Filter) ([]Object, error)
  }
```

### Skeleton Phases (Summary)

**Skeleton 0: Hello Context (Bootable Spine)**
- Goal: Prove end-to-end path exists
- Features: capture → store → retrieve
- Placeholders: In-memory storage, no pipelines, basic search
- Success: `ctxt add "hello" && ctxt find "hello"`

**Skeleton 1: Durable Core (dPKMS Minimal Runtime)**
- Goal: Make spine reliable
- Features: SQLite + WAL, jobs table, crash recovery
- Replaces: In-memory storage → SQLite
- Success: Survives process restart, jobs replay

**Skeleton 2: Meaning Spine (Entities + Mentions + Graph)**
- Goal: Storage becomes semantic
- Features: Entity schema, mention extraction, backlinks
- Contracts: Entity resolution, graph edges
- Success: `@mention` creates graph edge

**Skeleton 3: Daily Usability (Capture + Retrieve)**
- Goal: Frictionless enough to use daily
- Features: Multi-input types, inbox, status, export
- Replaces: Single input type → URL, file, stdin
- Success: Daily workflow feels natural

**Skeleton 4: Recipes, Not Frameworks (Built-in Pipelines)**
- Goal: Intelligence without breaking determinism
- Features: text.short, text.long, url.generic, image.ocr
- Contracts: Pipeline step interface, provenance
- Success: AI enrichment works, traceable

**Skeleton 5: Registry Subscriptions (Decentralized Semantics)**
- Goal: Federation without central dependency
- Features: Registry protocol, sync, merging, local overrides
- Contracts: Registry API, entity resolution
- Success: Multi-registry entity resolution

**Skeleton 6: Hybrid Retrieval (Discoverability Upgrades)**
- Goal: Forgiving search under uncertainty
- Features: Vector search, hybrid query, reranking
- Replaces: FTS-only → FTS + Vector + Graph
- Success: Semantic search finds results keyword search misses

**Skeleton 7: Profiles + Just-in-Time Context**
- Goal: Situationally relevant knowledge
- Features: Focus profiles, scoped registries, resurfacing
- Success: Knowledge surfaces based on context

**Skeleton 8: Trust That Travels (Optional Crypto)**
- Goal: Portable trust without mandatory crypto
- Features: Signed bundles, signed registries, trust policies
- Success: Trust without central authority

**Skeleton 9: Plugins + Ecosystem**
- Goal: Extend without forks
- Features: Plugin contracts, capability system, extension hooks
- Success: Third-party plugins work

**Skeleton 10: Scale Track (Optional Enterprise Mode)**
- Goal: Growth without sacrificing local-first
- Features: Postgres backend, distributed workers, ACL
- Success: Multi-user deployments work

### Contract Stability Enforcement

**Core Contracts (Stabilize Early):**

```go
// Storage Backend Contract (Skeleton 1)
type StorageBackend interface {
    Store(ctx context.Context, obj Object) error
    Retrieve(ctx context.Context, id string) (Object, error)
    Query(ctx context.Context, filter Filter) ([]Object, error)
    Delete(ctx context.Context, id string) error
}

// Pipeline Step Contract (Skeleton 4)
type PipelineStep interface {
    Name() string
    Execute(ctx Context, input any) (output any, error)
    Retryable() bool
    RequiredCapabilities() []Capability
}

// Entity Resolution Contract (Skeleton 5)
type EntityResolver interface {
    Resolve(ctx context.Context, slug string) (Entity, error)
    ResolveMany(ctx context.Context, slugs []string) ([]Entity, error)
    RegisterOverride(ctx context.Context, override EntityOverride) error
}

// Vector Backend Contract (Skeleton 6)
type VectorBackend interface {
    Store(ctx context.Context, id string, vector []float32) error
    Search(ctx context.Context, vector []float32, limit int) ([]SearchResult, error)
    Delete(ctx context.Context, id string) error
}
```

**Breaking Changes Forbidden:**

Once contract stabilized:
- No signature changes
- No semantic changes
- Extensions allowed (optional params, new methods)
- Deprecation allowed (with grace period)

**Version Strategy:**

```
Package: github.com/contexthelp/dpkms/v1
         github.com/contexthelp/dpkms/v2  # If breaking changes needed
```

### Placeholder Tracking

**Placeholder Annotation:**

```go
// PLACEHOLDER(skeleton-1): In-memory storage
// TODO(skeleton-1): Replace with SQLite backend
// Replacement planned for: Skeleton 1
type InMemoryStorage struct {
    objects map[string]Object
}
```

**Placeholder Registry:**

```yaml
placeholders:
  - id: in-memory-storage
    skeleton_created: 0
    skeleton_replacement: 1
    status: replaced
    replaced_by: sqlite-storage

  - id: basic-fts-search
    skeleton_created: 1
    skeleton_replacement: 6
    status: pending
    replacement_plan: hybrid-search
```

**Placeholder Lifecycle:**

1. **Created:** Marked with `PLACEHOLDER` comment + skeleton number
2. **Tracked:** Added to placeholders registry
3. **Replaced:** Implementation swapped, tests updated
4. **Validated:** Original tests pass with new implementation
5. **Removed:** Placeholder code deleted

### Phase Transition Criteria

**Exit Criteria for Each Skeleton:**

Each skeleton must meet criteria before moving to next:

```yaml
skeleton_0:
  exit_criteria:
    - [ ] End-to-end test passes (add → find → open)
    - [ ] Export produces valid bundle
    - [ ] README explains how to use it
    - [ ] CI runs successfully

skeleton_1:
  exit_criteria:
    - [ ] SQLite backend with WAL works
    - [ ] Jobs survive process restart
    - [ ] Crash recovery test passes
    - [ ] Migration system exists
    - [ ] Schema documented

skeleton_2:
  exit_criteria:
    - [ ] Mention extraction works
    - [ ] Entity resolution works
    - [ ] Backlinks queryable
    - [ ] Graph query tests pass
```

**No Skipping:**

Cannot start Skeleton N+1 until Skeleton N complete.

**Regression Prevention:**

All previous skeleton tests must pass.

### Contributor Workflow

**Clear Priorities:**

```
Current Skeleton: 2 (Entities + Mentions + Graph)
Status: 60% complete

In Progress:
- Entity schema (done)
- Mention extraction (in review)
- Backlinks index (in progress)

Next:
- Graph query API
- Entity resolution tests

Blocked:
- Registry sync (waiting for Skeleton 5)
```

**Contribution Guidelines:**

1. **Focus on Current Skeleton:** Contributions should target current skeleton goals
2. **Contract Changes Require Discussion:** Breaking changes to stabilized contracts need ADR
3. **Placeholders Explicit:** Mark any placeholders clearly
4. **Tests Required:** Each skeleton phase requires tests
5. **Documentation Updated:** README, architecture docs updated per skeleton

### Progress Measurement

**Skeleton Completion Metrics:**

```
Skeleton 0: ████████████████████ 100% (Shipped)
Skeleton 1: ████████████████░░░░  80% (In Progress)
Skeleton 2: ████░░░░░░░░░░░░░░░░  20% (Planning)
Skeleton 3: ░░░░░░░░░░░░░░░░░░░░   0% (Pending)
```

**Definition of Done:**

- [ ] Exit criteria met
- [ ] Tests pass (unit + integration + e2e)
- [ ] Documentation updated
- [ ] Previous skeleton tests still pass
- [ ] Placeholders tracked
- [ ] Demo/example works

---

## Consequences

### Positive

- **Working Software from Day One:** Users can adopt incrementally
- **Architectural Validation:** Real usage validates design decisions early
- **Contract Stability:** Interfaces stabilize early, implementations evolve safely
- **Feedback Loop:** Users inform priorities, not speculation
- **Contributor Clarity:** Clear goals, stable foundation
- **Progress Measurement:** Skeleton completion is objective metric
- **No Big-Bang Integration:** Continuous integration throughout
- **Reduced Rewrite Risk:** Replacement over rewrites

### Negative

- **Discipline Required:** Must resist feature creep between skeletons
- **Perceived Incompleteness:** Early skeletons look "simple" (intentional)
- **Placeholder Management:** Must track and systematically replace placeholders
- **Documentation Overhead:** Must document each skeleton phase

### Neutral

- **Longer Perceived Timeline:** Progress visible but incremental
- **Contract Evolution:** Some contracts may need v2 if initial design flawed

---

## Compliance

**Must Have for v1.0:**
- [ ] Skeleton 0-6 complete (Hello Context → Hybrid Retrieval)
- [ ] All exit criteria met for completed skeletons
- [ ] All placeholders tracked and replacement planned
- [ ] Regression tests pass for all previous skeletons
- [ ] Documentation reflects current skeleton state

**Should Have for v1.0:**
- [ ] Skeleton 7 complete (Profiles + Just-in-Time Context)
- [ ] Skeleton 8 planning complete (Trust That Travels)

**May Have for v2.0:**
- [ ] Skeleton 9 complete (Plugins + Ecosystem)
- [ ] Skeleton 10 complete (Scale Track)

---

## Notes

**Design References:**
- `ROADMAP.md:1-16` (Living Skeleton Method introduction)
- Alistair Cockburn's "Walking Skeleton" pattern
- Agile vertical slice development

**Related User Stories:**
- Developer wants to contribute but unclear on priorities
- User wants to adopt early, validates workflows
- Architect wants to validate design decisions before committing
- Project manager needs objective progress metrics

**Open Questions:**
- How to handle security/vulnerability fixes across skeleton phases?
- Should we support multiple active skeletons (parallel development)?
- How to version documentation per skeleton?

**Future Considerations:**
- Automated skeleton completion tracking
- Skeleton-specific release tags (v0.1-skeleton-1)
- Contributor skeleton assignment
