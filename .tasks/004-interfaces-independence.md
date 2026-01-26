# Skeleton 4: Interfaces & Independence Tasks

**Package Focus:** dPKMS (60%) + ctxt (40%)

**Goal:** Make dPKMS accessible via REST/gRPC APIs, add agent profiles for scoped worldviews.

---

## dPKMS Infrastructure Team (The Server)

### Unified Server Architecture

- [PLAN] 📦 *dPKMS* Implement `dpkms serve` multi-service runner
  - [ ] Single command runs:
    - [ ] Worker (ingestion job processor)
    - [ ] HTTP REST server (port 7700)
    - [ ] gRPC server (port 7701)
    - [ ] Plugin-injected interface hooks (middleware, handlers)
  - [ ] Graceful shutdown for all services
  - [ ] Independent failure recovery per service
  - [ ] Thread-safe plugin integration for request hooks
  - [ ] Health check endpoints

### REST API Implementation (ADR-012 aligned)

- [PLAN] 📦 *dPKMS* Create HTTP API in `dpkms/pkg/api/rest/`
  - [ ] Implement endpoints:
    - [ ] `POST /analyze` - enqueue ingestion job
    - [ ] `GET /jobs/{id}` - get job status
    - [ ] `GET /bookmarks` - list knowledge objects (query support)
    - [ ] `GET /bookmarks/{id}` - get single knowledge object
    - [ ] `GET /entities` - list entities (namespace filtering)
    - [ ] `GET /entities/{slug}` - get entity details
    - [ ] `GET /entities/{slug}/backlinks` - get knowledge objects referencing entity
    - [ ] Plugin-injected endpoints (via ADR-012 extension rules)
  - [ ] Middleware:
    - [ ] Basic auth (optional, configurable)
    - [ ] CORS support
    - [ ] Request logging
    - [ ] Plugin-defined interceptors (validated under permissions)
  - [ ] OpenAPI/Swagger documentation generation

### gRPC API Implementation

- [PLAN] 📦 *dPKMS* Define protocol buffers and services
  - [ ] Create `api-grpc.proto` with services:
    - [ ] `AnalyzeService` - job submission
    - [ ] `BookmarkService` - CRUD operations
    - [ ] `EntityService` - entity and graph operations
    - [ ] Service extension hooks for plugins (ADR-012)
  - [ ] Generate Go bindings from proto files
  - [ ] Implement service handlers
  - [ ] Support streaming for large result sets
  - [ ] Server startup includes dynamic plugin service registration

### Mention-Aware Query Handling

- [PLAN] 📦 *dPKMS* Extend API query capabilities
  - [ ] REST endpoint query parameters:
    - [ ] `?q=mention:<id>` - mention filter
    - [ ] `?q=mention:<pattern>*` - wildcard mention filter
    - [ ] `?entity=<id>` - entity filter
  - [ ] API must:
    - [ ] Call entity index for expansion
    - [ ] Resolve graph edges for transitive queries
    - [ ] Apply agent worldview restrictions
  - [ ] Plugin hook: allow plugins to define additional mention-based operators
  - [ ] Document query syntax in API docs

---

## ctxt Ingestion Team (The Logic)

### Agent Configuration Schema

- [PLAN] 📦 *ctxt* Update config loader for agent profiles
  - [ ] Parse `agents:` block from config:
    ```yaml
    agents:
      designer:
        registries: ["uxpatterns", "accessibility"]
        allowed_pipelines: ["url.generic", "image.ocr"]
        allowed_entities: ["ui.*", "design.*"]
        worldview_constraints:
          tags: ["design", "ux"]
        plugin_rules:
          semantic_augmentors: ["domain-concepts"]
    ```
  - [ ] Validate agent configuration on load
  - [ ] Support polymorphic plugin config (ADR-012)
  - [ ] Agent-specific LLM model selection

### Pipeline Agent-Awareness

- [PLAN] 📦 *ctxt* Modify pipeline context for agents
  - [ ] Update `PipelineContext` struct:
    ```go
    type PipelineContext struct {
        JobID       string
        AgentProfile *AgentProfile
        Config       *Config
    }
    ```
  - [ ] Agent profiles affect:
    - [ ] Pipeline selection (only allowed pipelines)
    - [ ] Registry priority order
    - [ ] Mention resolution order (scoped to allowed entities)
    - [ ] Plugin-defined overrides and augmentations
  - [ ] Validate agent permissions before execution

### Mention Extraction Step

- [PLAN] 📦 *ctxt* Standardize mention extraction across modalities
  - [ ] Create unified `MentionExtractor` component
  - [ ] Apply across all pipelines:
    - [ ] Text → detect `@namespace.slug` syntax
    - [ ] Transcripts → detect `@namespace.slug` syntax
    - [ ] OCR output → detect `@namespace.slug` syntax
    - [ ] Metadata fields → detect `@namespace.slug` syntax
  - [ ] Validate mention syntax:
    - [ ] Namespace must match pattern `[a-z0-9-]+`
    - [ ] Slug must match pattern `[a-z0-9-]+`
    - [ ] Full format: `@namespace.slug` or `@namespace.category.slug`
  - [ ] Return unified `mentions[]` array

### Entity Resolution Step

- [PLAN] 📦 *ctxt* Implement resolution cascade
  - [ ] Resolution priority order:
    1. [ ] Local entity definitions (user-defined)
    2. [ ] Registry-synced definitions (cached)
    3. [ ] Plugin-provided entity augmenters (ADR-012)
    4. [ ] Create local placeholder entity (stable ID generation)
  - [ ] Unresolved mentions become placeholders with:
    - [ ] Stable ID: `local.unresolved.<slug>`
    - [ ] Canonical namespace: `local.unresolved`
    - [ ] Promotion rules: upgrade to registry entity when available
  - [ ] Store provenance for all resolutions

### CLI Wait Mode

- [ ] 📦 *ctxt* Implement `ctxt analyze --wait` flag
  - [ ] Poll job status until completion
  - [ ] Display progress indicator
  - [ ] Show final status and result ID
  - [ ] Surface plugin notifications if Notification Plugin installed
  - [ ] Timeout after configurable duration

---

## dPKMS Search Team & ctxt Retrieval Team (The Filter)

### Scoped Search Implementation

- [PLAN] 📦 *dPKMS* Add agent-scoped query execution
  - [ ] `ListKnowledgeObjects` accepts `AgentID` parameter
  - [ ] Worldview restrictions applied:
    - [ ] Filter by allowed registries
    - [ ] Filter by allowed entity namespaces
    - [ ] Filter by allowed pipeline types
    - [ ] Filter by mention patterns
    - [ ] Apply plugin-defined filters (ADR-012)
  - [ ] Query results respect agent permissions
  - [ ] Performance optimization for scoped queries

### Mention-Aware Query Engine

- [PLAN] 📦 *dPKMS* Extend AST parsing for mentions
  - [ ] Parse mention operators:
    - [ ] `mention:<id>` - exact entity match
    - [ ] `mention:<namespace.*>` - namespace wildcard
    - [ ] `mention:*` - any mention present
  - [ ] Query resolution using:
    - [ ] Entity index lookup
    - [ ] Backlinks table traversal
    - [ ] Optional plugin semantic operators
  - [ ] Support negation: `NOT mention:deprecated.*`
  - [ ] Combine with FTS and tag filters

### Pagination Implementation

- [ ] 📦 *dPKMS* Add pagination support throughout stack
  - [ ] Update `Storage` interface:
    - [ ] Add `Limit` and `Offset` parameters
    - [ ] Return total count alongside results
  - [ ] SQL backend pagination:
    - [ ] Use `LIMIT` and `OFFSET` clauses
    - [ ] Optimize with covering indexes
  - [ ] REST API pagination:
    - [ ] Query params: `?limit=50&offset=0`
    - [ ] Response includes: `total`, `limit`, `offset`, `results`
  - [ ] gRPC pagination (proto message fields)
  - [ ] CLI flags: `--limit`, `--start`, `--page`

### Vector Store Interface Preparation

- [ ] 📦 *dPKMS* Define plugin-extensible vector interface
  - [ ] Create `VectorStore` interface:
    ```go
    type VectorStore interface {
        Store(ctx context.Context, id string, vector []float32) error
        Search(ctx context.Context, query []float32, k int) ([]Result, error)
        Delete(ctx context.Context, id string) error
    }
    ```
  - [ ] Plugins may register custom vector backends
  - [ ] Default implementation: in-memory or SQLite JSON
  - [ ] Prepare for Skeleton 5 vector search integration

---

## dPKMS Registry Team (The Sync)

### Registry Cache Table (ADR-009)

- [PLAN] 📦 *dPKMS* Implement local registry cache
  - [ ] Create `registry_cache` table schema:
    - [ ] `registry_url` (TEXT, indexed)
    - [ ] `data_type` (TEXT: 'taxonomy', 'entity', 'bookmark')
    - [ ] `data_key` (TEXT: entity ID or bookmark ID)
    - [ ] `data_value` (JSONB: full object)
    - [ ] `synced_at` (TIMESTAMP)
    - [ ] `version` (INTEGER)
    - [ ] `metadata` (JSONB: plugin extensions)
  - [ ] Indexes for fast lookup
  - [ ] TTL-based expiry support

### Sync Logic Implementation

- [PLAN] 📦 *ctxt* Implement `ctxt registry sync` command
  - [ ] Download registry data via HTTP:
    - [ ] Fetch taxonomy JSON
    - [ ] Fetch entities JSON
    - [ ] Fetch aliases JSON
  - [ ] Upsert into `registry_cache` table:
    - [ ] Taxonomies
    - [ ] Entities with provenance
    - [ ] Aliases and translations
    - [ ] Plugin extension data (if permitted by registry contract)
  - [ ] Conflict resolution:
    - [ ] Local entities override registry entities
    - [ ] Track version numbers
    - [ ] Warn on schema mismatches
  - [ ] Progress indicator for large registries

### Local-First Fallback

- [PLAN] 📦 *dPKMS* Implement offline registry access
  - [ ] Read entities from SQLite cache when offline
  - [ ] Read taxonomies from SQLite cache
  - [ ] Merge view strategy:
    - [ ] Local entities override cached registry entities
    - [ ] Plugin-defined alias layers apply last
    - [ ] Deterministic merge order
  - [ ] Stale data warnings (TTL expired)
  - [ ] Manual refresh command: `ctxt registry refresh`

---

## Integration Check (Offline Mention Resolution)

### Setup

1. [ ] Create Agent "Designer" in config:
   ```yaml
   agents:
     designer:
       registries: ["uxpatterns"]
       allowed_entities: ["ui.*", "design.*"]
   ```

2. [ ] Run registry sync:
   ```bash
   ctxt registry sync uxpatterns
   ```

3. [ ] Disconnect network (airplane mode or firewall rule)

### Scenario: API Ingestion While Offline

1. [ ] POST to REST API:
   ```json
   POST http://localhost:7700/analyze
   {
     "text": "Bad contrast violates @ui.accessibility.contrast",
     "agent": "Designer"
   }
   ```

2. [ ] Verify:
   - [ ] Job queued successfully
   - [ ] Worker processes pipeline
   - [ ] Mentions extracted: `@ui.accessibility.contrast`
   - [ ] Entity resolved from cached registry (offline)
   - [ ] Graph edges added to backlinks table
   - [ ] Plugins receive `post_ingest` hook event

### Scenario: CLI Retrieval with Mention Filter

1. [ ] Run:
   ```bash
   ctxt list --agent Designer --query 'mention:ui.accessibility.*'
   ```

2. [ ] Verify:
   - [ ] Query executes offline using cache
   - [ ] Entity graph traversal works
   - [ ] Results returned correctly
   - [ ] Agent worldview restrictions applied

### Validation Checklist

**dPKMS Validation:**
- [ ] REST and gRPC servers run concurrently with worker
- [ ] Entity-aware endpoints functional
- [ ] Mention-aware query support in APIs
- [ ] Registry cache persists offline
- [ ] Agent scoping works in queries
- [ ] Pagination functional across all interfaces

**ctxt Validation:**
- [ ] Agent configuration parsed correctly
- [ ] Pipeline agent-awareness works
- [ ] Mention extraction consistent across modalities
- [ ] Entity resolution uses cache when offline
- [ ] `--wait` flag polls job correctly

**Cross-Package Validation:**
- [ ] REST API talks to dPKMS storage
- [ ] gRPC API talks to dPKMS storage
- [ ] ctxt CLI uses APIs or direct storage
- [ ] Agent profiles apply across all interfaces

---

## Risks to Watch For

- **Concurrency Failures:** Worker/REST/gRPC/plugins running together require supervision
- **Schema Drift:** Registry cache includes entities, aliases, plugin metadata
- **Search Ambiguity:** Tag vs mention vs plugin operators must not collide
- **Resolution Conflicts:** Multiple registries defining same entity need deterministic precedence
- **Local Entity Pollution:** Too many unresolved mentions may degrade graph quality
- **Plugin Safety:** Plugins must not override core identity resolution unless permitted

---

## See Also

- Sprint spec: `docs/sprints/004-interface-and-independence.md`
- ADR-009: Registry sync and caching
- ADR-012: Plugin contract (revised)
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
