# Skeleton 5: Semantics & Sidecars Tasks

**Package Focus:** dPKMS (50%) + ctxt (50%)

**Goal:** Semantic search via embeddings + plugin system for extensibility. Mentions and entities drive semantic identity.

---

## dPKMS Infrastructure Team (The Host)

### Plugin Interface Definitions (ADR-012)

- [PLAN] 📦 *dPKMS* Define plugin contracts in `dpkms/pkg/plugin/interfaces.go`
  - [ ] `PluginPipelineStep` interface:
    ```go
    type PluginPipelineStep interface {
        Name() string
        Execute(ctx context.Context, input PipelineInput) (PipelineOutput, error)
    }
    ```
  - [ ] `PluginCommand` interface:
    ```go
    type PluginCommand interface {
        Name() string
        Description() string
        Execute(ctx context.Context, args []string) error
    }
    ```
  - [ ] `PluginSemanticAugmentor` interface (NEW):
    ```go
    type PluginSemanticAugmentor interface {
        // May emit mentions, local entities, aliases, metadata
        Augment(ctx context.Context, obj KnowledgeObject) (Augmentation, error)
    }
    ```
  - [ ] **Contract requirements:**
    - [ ] Plugins may emit mentions (`@concept.id`)
    - [ ] Plugins may create namespaced local entities (`plugin.myplugin.entityName`)
    - [ ] Plugins may propose alias suggestions
    - [ ] Plugins may attach metadata enrichments
    - [ ] **Plugins MUST NOT overwrite canonical entity IDs**
    - [ ] **Plugins MUST NOT mutate registry-provided definitions**
    - [ ] **Plugins MUST NOT modify mention/entity schema tables directly**
  - [ ] Document semantic-safe boundaries

### Plugin Loader Implementation

- [PLAN] 📦 *dPKMS* Implement plugin discovery and loading
  - [ ] Discover plugin binaries in `~/.config/contexthelp/plugins/`
  - [ ] Load plugins in isolated process or RPC mode (ADR-012)
  - [ ] Validate plugin manifest (`manifest.yaml`)
  - [ ] Enforce semantic-safe boundaries:
    - [ ] Plugins cannot override existing entity records
    - [ ] Plugins cannot rename or delete canonical entities
    - [ ] Plugins cannot access entity/mention tables directly
  - [ ] Safely allow:
    - [ ] Local entity creation (namespaced)
    - [ ] Sidecar-based semantic enrichment via interfaces
    - [ ] Plugin-provided graph edges (stored in plugin namespaces)
  - [ ] Plugin lifecycle management (init, execute, shutdown)
  - [ ] Error isolation (plugin crash doesn't kill dPKMS)

### Polymorphic Config Update

- [PLAN] 📦 *dPKMS* Extend configuration system for plugins
  - [ ] Route config blocks to relevant plugin types
  - [ ] Add support for:
    - [ ] `semanticAugmentor:` namespace in config
    - [ ] Plugin-controlled semantic augmentation toggle
    - [ ] Per-plugin namespace policies (which entity namespaces they may emit)
    - [ ] Limits on emitted mentions or entity nodes
  - [ ] Example config:
    ```yaml
    plugins:
      domain-concepts:
        type: semantic_augmentor
        enabled: true
        namespaces: ["plugin.domain-concepts.*"]
        max_mentions_per_object: 10
    ```
  - [ ] Validate plugin config against manifest

---

## ctxt Ingestion Team (The Embedder)

### Embedding Client Interface

- [PLAN] 📦 *ctxt* Create `EmbeddingClient` in `ctxt/pkg/ai/embedding.go`
  - [ ] Define interface:
    ```go
    type EmbeddingClient interface {
        Embed(ctx context.Context, text string) ([]float32, error)
        EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
        Dimensions() int
    }
    ```
  - [ ] Implement adapters:
    - [ ] OpenAI embeddings
    - [ ] Local ONNX model
    - [ ] Ollama embeddings
  - [ ] Caching decorator for deterministic embeddings (hash-based)
  - [ ] **Guarantee processing order:**
    1. [ ] Mention extraction
    2. [ ] Entity resolution
    3. [ ] Text chunking
    4. [ ] Embedding generation
  - [ ] Batch processing for efficiency

### Pipeline Updates for Vector Generation

- [PLAN] 📦 *ctxt* Update `text.long` and `url.generic` pipelines
  - [ ] New processing order:
    1. [ ] Extract mentions (`@namespace.id`) from text
    2. [ ] Resolve each mention into entity:
       - [ ] Registry → local → plugin-defined namespaces
    3. [ ] Allow semantic sidecars (plugins) to:
       - [ ] Propose additional mentions
       - [ ] Suggest alias relationships
       - [ ] Attach metadata to local entities
    4. [ ] Chunk long text consistently:
       - [ ] Configurable chunk size (e.g., 512 tokens)
       - [ ] Overlap between chunks (e.g., 50 tokens)
       - [ ] Preserve mention context across chunks
    5. [ ] Generate embeddings per chunk
    6. [ ] Attach to KnowledgeObject:
       - [ ] `mentions[]` array
       - [ ] Resolved `entity_ids[]`
       - [ ] `vectors[]` (one per chunk)
       - [ ] Backlink updates
  - [ ] Store chunk metadata (position, overlap)
  - [ ] Handle embedding API errors gracefully

### Storage Schema Update for Vectors

- [PLAN] 📦 *dPKMS* Extend schema for vector storage
  - [ ] Create `vectors` table:
    - [ ] `knowledge_object_id` (TEXT, indexed)
    - [ ] `chunk_index` (INTEGER)
    - [ ] `vector` (BLOB or JSON array, configurable)
    - [ ] `dimensions` (INTEGER)
    - [ ] `model` (TEXT: which embedding model used)
    - [ ] `created_at` (TIMESTAMP)
  - [ ] Update `knowledge_objects` table:
    - [ ] Ensure `mentions[]` field exists
    - [ ] Add `has_vectors` (BOOLEAN) flag for query optimization
  - [ ] Update `entities` table (if not already present):
    - [ ] `id`, `title`, `namespace`, `aliases`, `metadata`
  - [ ] Update `entity_backlinks` table:
    - [ ] Composite index on (entity_id, knowledge_object_id)
  - [ ] Configurable vector storage format:
    - [ ] JSON arrays (human-readable, larger)
    - [ ] Binary blobs (compact, faster)
  - [ ] Migration script for existing data

---

## dPKMS Search Team & ctxt Retrieval Team (The Mathematician)

### Vector Search Implementation

- [PLAN] 📦 *dPKMS* Implement baseline vector search
  - [ ] In-memory cosine similarity baseline:
    - [ ] Load all vectors from database
    - [ ] Compute cosine similarity with query vector
    - [ ] Return top-k results
  - [ ] **Apply mention/entity filters BEFORE vector scoring:**
    - [ ] Filter by mention presence
    - [ ] Filter by entity ID
    - [ ] Filter by namespace pattern
  - [ ] Respect query operators:
    - [ ] Positive filters: `mention:ui.best-practice`
    - [ ] Negative filters: `NOT mention:deprecated.*`
    - [ ] Wildcard patterns: `mention:stripe.api.*`
  - [ ] Performance optimization:
    - [ ] Early exit for filtered sets
    - [ ] Batch similarity computation
  - [ ] Return results with similarity scores

### RSQL Extension for Mentions

- [PLAN] 📦 *dPKMS* Extend query parser for mention operators
  - [ ] Support syntax:
    - [ ] `mention:"ui.best-practice"` - exact match
    - [ ] `mention:stripe.api.*` - namespace wildcard
    - [ ] `NOT mention:deprecated.entity` - negation
    - [ ] `mention:*` - any mention present
  - [ ] Behavior:
    - [ ] Extract query-time mentions
    - [ ] Resolve query mention → canonical entity
    - [ ] Retrieve linked knowledge objects via:
      - [ ] Backlinks table
      - [ ] Mentions JSON field
    - [ ] Combine with FTS and vector results
  - [ ] Query optimization:
    - [ ] Use entity index for fast lookups
    - [ ] Limit graph traversal depth
    - [ ] Cache resolved entities

### Reciprocal Rank Fusion (RRF)

- [PLAN] 📦 *dPKMS* Implement hybrid ranking
  - [ ] RRF inputs now include:
    - [ ] FTS Rank (from FTS5)
    - [ ] Vector Rank (cosine similarity)
    - [ ] Mention/Entity Rank (graph-based)
    - [ ] Registry-provided weight multipliers
  - [ ] Base RRF formula:
    ```
    Score = 1/(k + FTS_Rank) + 1/(k + Vector_Rank)
    ```
    where k = 60 (standard constant)
  - [ ] **Extend with semantic signals:**
    - [ ] `+ mentionBoost` - explicit mention of target entity
    - [ ] `+ entityAffinityScore` - graph adjacency/closeness
    - [ ] `+ registryWeight` - registry-defined importance
  - [ ] Configurable weights per signal
  - [ ] Return score breakdown for explainability
  - [ ] Normalize final scores to [0, 1] range

---

## dPKMS Registry Team (The Polisher)

### Weight Application Rules

- [PLAN] 📦 *dPKMS* Implement registry weight influence
  - [ ] Registry-provided weights affect:
    - [ ] Tag scoring (tag importance multiplier)
    - [ ] Entity scoring (entity relevance boost)
    - [ ] Semantic reranking (when entities involved)
  - [ ] Apply weight rules per:
    - [ ] Registry priority (user-configured order)
    - [ ] Namespace isolation (weights don't cross namespaces)
    - [ ] Alias relationships (aliases inherit base entity weight)
  - [ ] Conflict resolution:
    - [ ] Higher priority registry wins
    - [ ] Local weights override remote
  - [ ] Performance: cache computed weights

### I18N Schema Support

- [PLAN] 📦 *dPKMS* Implement internationalization
  - [ ] Add `translations` field to knowledge_objects:
    ```json
    {
      "translations": {
        "en": {"title": "...", "summary": "..."},
        "es": {"title": "...", "summary": "..."}
      }
    }
    ```
  - [ ] Add `translations` field to entities:
    ```json
    {
      "translations": {
        "en": {"title": "Best Practice", "description": "..."},
        "es": {"title": "Mejor Práctica", "description": "..."}
      }
    }
    ```
  - [ ] RegistryClient returns localized labels
  - [ ] Preferred language affects display, not mention syntax
  - [ ] **Canonical entity IDs remain stable regardless of language**
  - [ ] Fallback chain: preferred → English → slug

### Entity Syncing Implementation

- [PLAN] 📦 *dPKMS* Implement entity synchronization from registries
  - [ ] Registry endpoints provide:
    - [ ] Entity definitions (id, title, description, aliases)
    - [ ] Alias lists (multiple slugs → same entity)
    - [ ] Merged metadata (provenance, version, tags)
    - [ ] Entity versions (for change tracking)
  - [ ] Merging strategy:
    - [ ] Local entity overrides remote if conflict
    - [ ] Registry priority determines tie-breaking
    - [ ] **Canonical IDs remain stable** (never reassign)
    - [ ] **Namespaced IDs avoid collisions** (registry.namespace.slug)
  - [ ] Track entity provenance:
    - [ ] Origin registry URL
    - [ ] Sync timestamp
    - [ ] Version number
  - [ ] Validate entity schema on sync
  - [ ] Log conflicts and resolution decisions

---

## Integration Check (Semantic Plugin + Mentions + Vector)

### Setup

1. [ ] Install plugins:
   - [ ] Slack Notifier plugin
   - [ ] Domain Concepts plugin (adds local entities, emits mentions)

2. [ ] Configure:
   ```yaml
   plugins:
     slack:
       webhook: "https://hooks.slack.com/..."
     domain-concepts:
       enabled: true
       namespaces: ["plugin.domain.*"]
   ```

### Scenario: Ingestion with Plugin Augmentation

1. [ ] Run:
   ```bash
   ctxt analyze --text "Our checkout flow needs to meet Stripe API best practices."
   ```

2. [ ] Verify pipeline steps:
   - [ ] Detect mentions: `@stripe.api.checkout`, `@ui.best-practice`
   - [ ] Resolve entities from registry (canonical IDs)
   - [ ] Plugin semantic augmentor adds domain-specific mentions
   - [ ] Plugin proposes alias: `stripe.checkout` → `stripe.api.checkout`
   - [ ] Chunking applied (if text long enough)
   - [ ] Embeddings generated per chunk
   - [ ] Bookmark stored with:
     - [ ] `mentions: ["stripe.api.checkout", "ui.best-practice", ...]`
     - [ ] `entity_ids: ["stripe.api.checkout", "ui.best-practice"]`
     - [ ] Vectors in `vectors` table
     - [ ] Backlinks in `entity_backlinks` table

### Scenario: Mention-Based Search

1. [ ] Run:
   ```bash
   ctxt list --q 'mention:stripe.api.*'
   ```

2. [ ] Verify behavior:
   - [ ] Mention filter yields direct matches
   - [ ] Graph traversal elevates related bookmarks (via shared entities)
   - [ ] Vector search provides semantic reinforcement
   - [ ] RRF combines FTS + vector + mention signals
   - [ ] Result: Bookmark ranked at top due to explicit entity alignment

### Validation Checklist

**dPKMS Validation:**
- [ ] Plugin loader discovers and loads plugins
- [ ] Plugin manifest validation works
- [ ] Semantic augmentor plugins can emit mentions
- [ ] Plugins cannot override canonical entities
- [ ] Vector storage persists embeddings
- [ ] RRF combines multiple ranking signals
- [ ] Entity weight application functional

**ctxt Validation:**
- [ ] Embedding client generates vectors
- [ ] Processing order: mentions → entities → chunking → embedding
- [ ] Pipelines integrate semantic augmentation
- [ ] Plugin-proposed mentions validated and stored
- [ ] Chunk metadata preserved

**Cross-Package Validation:**
- [ ] Plugins extend both dPKMS and ctxt capabilities
- [ ] Entity resolution uses registry + plugin augmentation
- [ ] Vector search integrates with mention filters
- [ ] RRF produces unified relevance scores

---

## Risks to Watch For

### Plugin Risks
- Plugins must not override canonical entity IDs
- Plugins may emit too many mentions (need rate limits)
- Sidecar entity proposals may create namespace clutter
- Plugin crashes must not affect core system

### Vector + Entity Interactions
- Reranking may overweight entity proximity unless balanced
- Circular alias relationships need detection
- Unresolved mentions must not pollute graph
- Vector dimensions must match across all embeddings

### Ingestion Risks
- Early mention extraction may cause unnecessary resolution calls
- Plugin augmentation must be bounded for performance
- Chunk boundaries may split mention context
- Embedding API rate limits need handling

### Graph Integrity
- Backlink creation must validate entity existence
- Plugin-generated edges must respect namespace constraints
- Corruption risks if plugins emit malformed edges
- Entity alias chains need cycle detection

---

## See Also

- Sprint spec: `docs/sprints/005-semantics-sidecars.md`
- ADR-012: Plugin contract (semantic safety)
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
