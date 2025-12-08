# Sprint 4 Goal: "Interfaces & Independence"

By the end of this sprint:
1. Developers can talk to ContextHelp via **HTTP/REST** or **gRPC** (not just CLI).
2. Users can define **Agents** that restrict search results to specific worldviews.
3. Registries can be **Synced** locally for offline use (ADR-009).
4. Interfaces become **mention-aware**, supporting canonical entities, backlinks, and knowledge-graph-powered filtering.
5. The system is fully aligned with **ADR-012 (Revised Plugin Contract)** so plugins can extend interfaces safely without modifying core.

---

## 1. Core/Infra Team (The Server)

**Focus:** The API Surface (REST & gRPC), expanded to support entity, mention, and graph operations, while exposing extension hooks defined by the plugin contract.
**Why:** Interfaces must expose structured semantic navigation *and* remain plugin-extendable.

### Task 1.1: The Unified Server (`ch serve`)

- Run:
  - Worker (Ingestion)
  - HTTP REST server
  - gRPC server
  - Plugin-injected interface hooks (e.g., middleware, pre/post handlers)
- Deliverable: Running `ch serve` opens:
  - port 7700 (HTTP)
  - port 7701 (gRPC)
- Ensure:
  - graceful shutdown
  - independent failure recovery
  - thread-safe plugin integration for request hooks

### Task 1.2: REST Implementation (ADR-012 aligned)

Endpoints:

- `POST /analyze`
- `GET /jobs/{id}`
- `GET /bookmarks`
- `GET /entities`
- `GET /entities/{slug}`
- `GET /entities/{slug}/backlinks`
- *Plugin-injected endpoints* (via ADR-012 extension rules)

Middleware:

- Basic auth (optional)
- CORS
- Plugin-defined interceptors (validated under permissions)

### Task 1.3: gRPC Implementation

Define `api-grpc.proto` with:

- `AnalyzeService`
- `BookmarkService`
- `EntityService`
- *Service extension hooks* for plugins (ADR-012: gRPC method registration)

Generate Go bindings and ensure server startup includes dynamic plugin service registration.

### Task 1.4: Mention-Aware Query Handling

- Add support for:
  - `mention:<id>`
  - `mention:<pattern>*`
- REST + gRPC must:
  - call entity index for expansion
  - resolve graph edges for transitive queries
- Plugin hook: allow plugins to define additional mention-based operators.

---

## 2. Ingestion/AI Team (The Logic)

**Focus:** Agent Profiles, Worldviews, Mention Extraction, Entity Resolution, and Plugin-Safe Semantic Extensions.
**Why:** Semantic identity must be consistent, deterministic, and extensible.

### Task 2.1: Agent Configuration Schema

Update the config loader:

- Parse `agents:` block with:
  - registry sets
  - allowed pipelines
  - allowed entities
  - worldview constraints
  - plugin-defined semantic rules (ADR-012 polymorphic config)

### Task 2.2: Pipeline Agent-Awareness

Modify `PipelineContext` to accept an `AgentProfile`.

Agent profiles affect:

- pipeline selection
- registry priorities
- mention resolution order
- plugin-defined overrides

### Task 2.3: Mention Extraction Step

Apply across all modalities:

- text → @syntax
- transcripts → @syntax
- OCR → @syntax
- metadata → @syntax

Pipeline emits a unified `mentions[]` field.

### Task 2.4: Entity Resolution Step

Resolve mentions using:

1. local entity definitions
2. registry-synced definitions
3. plugin-provided entity augmenters (ADR-012)

Unresolved mentions become local placeholder entities with:

- stable IDs
- canonical namespace
- promotion rules

### Task 2.5: `ch analyze --wait`

CLI polls job until finished.

Also surface pending plugin notifications (if Notification Plugin installed).

---

## 3. Search/Retrieval Team (The Filter)

**Focus:** Mention-Aware Search, Agent Scoping, Graph Navigation, and Plugin-Safe Query Extensions.
**Why:** Retrieval must fully understand semantic identity and entity graph structure.

### Task 3.1: Scoped Search

`ListBookmarks` accepts an `AgentID`.

Worldview restricts:

- registries
- entities
- pipelines
- mentions
- plugin-defined filters (ADR-012)

### Task 3.2: Mention-Aware Query Engine

Extend AST to parse:

- `mention:<id>`
- `mention:<namespace.*>`

Resolve queries using:

- entity index
- backlinks table
- optional plugin semantic operators

### Task 3.3: Pagination

Add `Limit` + `Offset` to:

- Storage interface
- SQL backend
- REST/gRPC
- CLI flags: `--limit`, `--start`

### Task 3.4: Vector Interface (Prep)

Define `VectorStore` interface (plugin-extensible).

Plugins may register their own vector backends.

---

## 4. Registry/Ecosystem Team (The Sync)

**Focus:** Local Sync (ADR-009), expanded to include entity sync, alias resolution, and plugin-defined registry enrichers.
**Why:** Offline mention resolution requires registry-backed data.

### Task 4.1: The `registry_cache` Table

Stores:

- taxonomies
- bookmarks
- entities
- aliases
- plugin-provided metadata extensions

### Task 4.2: Sync Logic (`ch registry sync`)

- Download registry JSON
- Upsert:
  - taxonomies
  - entities
  - aliases
  - plugin extensions (if permitted by registry contract)

### Task 4.3: Local-First Fallback

- Read entities and taxonomies from SQLite when offline
- Merge view:
  - local entities override registry entities
  - plugin-defined alias layers apply afterward

---

## The Integration Check (The Demo)

**Scenario:** The "Offline Mention Resolution" Test.

**Setup**

- Create Agent “Designer” using “UXPatterns” registry
- Run `ch registry sync uxpatterns`
- Disconnect network

**Action**
```
POST http://localhost:7700/analyze
{"text": "Bad contrast violates @ui.accessibility.contrast", "agent": "Designer"}
```

**Result**

- Job queued → worker processes pipeline
- Mentions extracted
- Entity resolved from cached registry
- Graph edges added
- Plugins receive `post_ingest` hook event

**CLI Retrieval**

```
ch list --agent Designer --query 'mention:ui.accessibility.*'
```

Returns the bookmark via entity graph traversal.

---

## Risks to Watch For

- **Concurrency Failures:** Worker/REST/gRPC/plugin hooks running together require supervision.
- **Schema Drift:** Registry cache includes entities, aliases, plugin metadata.
- **Search Ambiguity:** Tag vs mention vs plugin-defined operators must not collide.
- **Resolution Conflicts:** Multiple registries defining same entity require deterministic precedence.
- **Local Entity Pollution:** Too many unresolved mentions may degrade graph quality without promotion workflows.
- **Plugin Safety:** Plugins must not override or shadow core identity resolution unless explicitly permitted.