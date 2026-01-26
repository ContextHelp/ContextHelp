# Skeleton 7: Sovereign & Scalable Tasks

**Package Focus:** dPKMS (50%) + ctxt (50%)

**Goal:** True offline AI (Ollama, LocalAI) + vector scalability (sqlite-vec/HNSW) + developer tooling. Deterministic entity governance across multi-registry environments.

---

## dPKMS Infrastructure Team (The Optimizer)

### Performance Profiling with pprof

- [PLAN] 📦 *dPKMS* Profile and optimize system under load
  - [ ] Profile ingestion with semantic workload:
    - [ ] 1,000+ URLs with mention extraction
    - [ ] Multimodal content (text, images, audio)
    - [ ] Entity resolution enabled (registry + local)
    - [ ] Graph updates (backlink creation)
  - [ ] CPU profiling:
    - [ ] Identify hot paths in mention extraction
    - [ ] Entity resolution bottlenecks
    - [ ] Graph traversal performance
  - [ ] Memory profiling:
    - [ ] Entity cache memory usage
    - [ ] Vector storage memory footprint
    - [ ] Plugin memory leaks
  - [ ] Optimize:
    - [ ] WAL checkpoint cadence (SQLite)
    - [ ] Mutex contention during entity/backlink writes
    - [ ] Pipeline worker goroutine pressure
    - [ ] Entity index update batching
  - [ ] Ensure entity operations don't stall ingestion
  - [ ] Target: <100ms p99 latency for mention resolution

### Binary Distribution

- [PLAN] 📦 *dPKMS* Setup GoReleaser for multi-platform builds
  - [ ] Configure GoReleaser (`.goreleaser.yaml`)
  - [ ] CI publishes signed binaries for:
    - [ ] macOS Universal (ARM64 + AMD64)
    - [ ] Linux (AMD64, ARM64, ARM)
    - [ ] Windows (AMD64)
  - [ ] Deliverables:
    - [ ] Homebrew formula (`brew install contexthelp`)
    - [ ] Manual downloads (GitHub Releases)
    - [ ] Checksum table (SHA256)
    - [ ] GPG signatures for verification
  - [ ] Version in binary: `ctxt version` shows commit, date, platform
  - [ ] Automated release on git tag push

### Strict Offline Mode

- [ ] 📦 *ctxt* Implement `ctxt config --offline` flag
  - [ ] Configuration:
    ```yaml
    offline_mode: true
    ```
  - [ ] Behavior when offline:
    - [ ] **Hard-block all outbound network requests**
    - [ ] Registries do not sync (use cached data only)
    - [ ] Plugins requiring networking denied unless explicitly permitted
    - [ ] **Mention resolution:**
      - [ ] Only local entity registry used
      - [ ] Unresolved mentions → stub entities (local namespace)
      - [ ] No fallback to remote registries
    - [ ] Pipelines degrade gracefully:
      - [ ] Skip remote API calls (LLM, embeddings)
      - [ ] Use local models (Ollama) if configured
      - [ ] Mark jobs as `NeedsHumanInput` if remote-only
  - [ ] Status indicator: `ctxt status` shows "Offline Mode: Enabled"
  - [ ] Toggle on/off without config edit

### Entity/Graph Storage Isolation

- [PLAN] 📦 *dPKMS* Implement fine-grained concurrency control
  - [ ] Add write locks for critical tables:
    - [ ] `entities` table (INSERT, UPDATE, DELETE)
    - [ ] `entity_aliases` table (alias mapping)
    - [ ] `entity_backlinks` table (graph edges)
  - [ ] Lock granularity:
    - [ ] Row-level where possible (SQLite supports via WAL)
    - [ ] Table-level for schema changes
    - [ ] Read locks for queries (shared)
    - [ ] Write locks for mutations (exclusive)
  - [ ] Ensure graph updates are amortized:
    - [ ] Batch backlink inserts (not O(n) per mention)
    - [ ] Use UPSERT to avoid duplicate checking overhead
    - [ ] Deferred constraint checking where safe
  - [ ] Guarantee referential integrity:
    - [ ] Foreign key constraints enabled
    - [ ] Cascade deletes for entity removal
    - [ ] Worker crash recovery validates graph consistency
  - [ ] Performance target: graph updates <10ms p99

---

## ctxt Ingestion Team (The Localist)

### Ollama Client Implementation

- [PLAN] 📦 *ctxt* Implement local model support
  - [ ] Implement `OllamaClient` for both interfaces:
    - [ ] `LLMClient` (text completion)
    - [ ] `EmbeddingClient` (vector generation)
  - [ ] Configuration:
    ```yaml
    ai_providers:
      ollama:
        enabled: true
        url: http://localhost:11434
        models:
          completion: llama2
          embedding: nomic-embed-text
    ```
  - [ ] Features:
    - [ ] Model pull on first use (if not present)
    - [ ] Streaming support for completions
    - [ ] Batch embedding requests
    - [ ] Timeout configuration per model
  - [ ] Allow per-pipeline model mapping:
    ```yaml
    pipelines:
      text.long:
        llm: ollama/llama2
        embedding: ollama/nomic-embed-text
    ```
  - [ ] Fallback order: Ollama → Cloud → Rule-based

### Local Fallback Strategy

- [ ] 📦 *ctxt* Implement graceful degradation
  - [ ] Resolution priority order:
    1. [ ] Cloud models (OpenAI, Anthropic) if online
    2. [ ] Local models (Ollama, LocalAI) if configured
    3. [ ] Rule-based extraction (regex, heuristics)
    4. [ ] Raw text preservation (no enrichment)
  - [ ] **Mention extraction runs regardless of model availability:**
    - [ ] Regex-based detection: `@namespace.slug` pattern
    - [ ] Confidence scoring (model-based vs rule-based)
    - [ ] Log extraction method for debugging
  - [ ] Pipeline behavior:
    - [ ] `text.short`: fallback to truncation if no LLM
    - [ ] `text.long`: fallback to section splitting
    - [ ] Embeddings: skip if no model (mark for reprocessing)
  - [ ] Job status reflects fallback: `CompletedWithFallback`

### Prompt Stability for Small Models

- [PLAN] 📦 *ctxt* Optimize prompts for local models
  - [ ] Design prompts for deterministic extraction:
    - [ ] Summaries (concise, bullet-point format)
    - [ ] Sections (heading detection, structure)
    - [ ] Decisions (action items, choices made)
    - [ ] Tags (category classification)
    - [ ] Hints (user-provided context)
    - [ ] **Mentions (`@namespace.id` syntax detection)**
  - [ ] Test across model sizes:
    - [ ] 7B parameter models (llama2, mistral)
    - [ ] 13B parameter models (better quality)
    - [ ] Quantized models (Q4, Q5, Q8)
  - [ ] Apply across modalities:
    - [ ] Plain text
    - [ ] OCR output (noisy)
    - [ ] Transcripts (speech patterns)
    - [ ] Metadata fields
  - [ ] Validate output format:
    - [ ] JSON schema validation
    - [ ] Fallback parser for malformed output
  - [ ] Performance target: <5s per extraction on local hardware

### Mention Extraction Stability

- [ ] 📦 *ctxt* Enforce strict mention syntax and validation
  - [ ] Mention pattern: `@namespace.slug` or `@namespace.category.slug`
  - [ ] Validation rules:
    - [ ] Namespace: lowercase, alphanumeric, hyphens only
    - [ ] Slug: lowercase, alphanumeric, hyphens only
    - [ ] Max length: 64 characters total
    - [ ] No consecutive hyphens
  - [ ] Error handling:
    - [ ] Surface structured errors for malformed mentions
    - [ ] Suggest corrections: `@UI.Button` → `@ui.button`
    - [ ] Log violations for debugging
  - [ ] False positive detection:
    - [ ] Ignore email addresses
    - [ ] Ignore social media handles (unless registry-defined)
    - [ ] Context-aware filtering (code blocks, URLs)
  - [ ] Performance: <1ms per mention validation

---

## dPKMS Search Team & ctxt Retrieval Team (The Architect)

### sqlite-vec Integration

- [PLAN] 📦 *dPKMS* Implement optimized vector storage
  - [ ] Option 1: `sqlite-vec` extension (if CGO acceptable)
    - [ ] Compile extension with SQLite
    - [ ] Load extension on database connection
    - [ ] Use native vector similarity functions
    - [ ] Test across platforms (macOS, Linux, Windows)
  - [ ] Option 2: Pure-Go HNSW index
    - [ ] In-memory index with disk persistence
    - [ ] Build HNSW graph on startup
    - [ ] Incremental updates on new embeddings
    - [ ] Fallback if CGO unavailable
  - [ ] Configuration:
    ```yaml
    vector_backend: sqlite-vec  # or hnsw-go
    vector_index:
      dimensions: 384
      metric: cosine
      hnsw_m: 16
      hnsw_ef_construction: 200
    ```
  - [ ] Performance target: <100ms for top-100 similarity search

### Vector Compression

- [PLAN] 📦 *dPKMS* Implement embedding quantization
  - [ ] Quantization methods:
    - [ ] int8 quantization (8x smaller, minimal quality loss)
    - [ ] Binary quantization (32x smaller, significant loss)
    - [ ] Product quantization (configurable trade-off)
  - [ ] Configuration per embedding model:
    ```yaml
    embeddings:
      openai:
        dimensions: 1536
        quantization: int8
      ollama:
        dimensions: 384
        quantization: none  # already small
    ```
  - [ ] Backward compatibility:
    - [ ] Store quantization method in metadata
    - [ ] Support mixed-precision queries
    - [ ] Migration path for existing vectors
  - [ ] Ensure plugin-provided backends support quantization API

### Re-indexing Command

- [ ] 📦 *ctxt* Implement `ctxt jobs reindex-vectors` command
  - [ ] Tasks performed:
    1. [ ] Recompute embeddings for all knowledge objects
       - [ ] Respect original embedding model
       - [ ] Update to new model if configured
    2. [ ] Update entity aggregate vectors:
       - [ ] Compute centroid of backlinked objects
       - [ ] Store in `entity_vectors` table (new)
    3. [ ] Rebuild graph-derived similarity fields:
       - [ ] Entity-to-entity similarity (via shared objects)
       - [ ] Update cached similarity scores
    4. [ ] Rebuild vector indexes (HNSW graph)
  - [ ] Options:
    - [ ] `--model <name>` - change embedding model
    - [ ] `--filter <query>` - reindex subset only
    - [ ] `--batch-size <n>` - control memory usage
  - [ ] Progress tracking:
    - [ ] Display: "Reindexing: 1234/5678 objects (21%)"
    - [ ] Pause/resume support (save checkpoint)
  - [ ] Background job (can run while system operational)

### Mention- and Entity-Aware Ranking

- [PLAN] 📦 *dPKMS* Enhance ranking with graph signals
  - [ ] Ranking boost factors:
    - [ ] **Direct mention match:** +0.5 score (object mentions queried entity)
    - [ ] **Graph proximity:** +0.1 to +0.3 (based on backlink distance)
      - [ ] 1 hop: +0.3
      - [ ] 2 hops: +0.2
      - [ ] 3 hops: +0.1
    - [ ] **Entity popularity:** +0.1 (highly-backlinked entities)
    - [ ] **Registry weight:** 1.0x to 2.0x multiplier
  - [ ] Graph expansion strategies:
    - [ ] Via backlinks (entity → objects referencing it)
    - [ ] Via alias-linked entities (aliases map to same entity)
    - [ ] Via registry-derived relations (taxonomy hierarchy)
  - [ ] `--expand-entities` flag:
    - [ ] Level 0: exact mention match only
    - [ ] Level 1: 1-hop graph expansion (default)
    - [ ] Level 2: 2-hop expansion
    - [ ] Level 3: 3-hop expansion (slowest, broadest)
  - [ ] Configurable weights:
    ```yaml
    ranking:
      mention_boost: 0.5
      graph_proximity_weight: 0.2
      entity_popularity_weight: 0.1
    ```

---

## dPKMS Registry Team (The Enabler)

### Registry Linter Tool

- [PLAN] 📦 *ctxt* Implement `ctxt dev validate-registry` command
  - [ ] Validate registry files:
    - [ ] `taxonomy.json` - tag hierarchy and metadata
    - [ ] `entities.json` - entity definitions
    - [ ] `aliases.json` - alias mappings
    - [ ] `translations/*.json` - i18n files
  - [ ] JSON schema validation:
    - [ ] Enforce required fields (id, title, namespace)
    - [ ] Validate field types and formats
    - [ ] Check version field consistency
  - [ ] Namespace rules:
    - [ ] No reserved prefixes (local, plugin, system)
    - [ ] Consistent namespace across files
    - [ ] Valid namespace pattern: `[a-z0-9-]+`
  - [ ] Alias validation:
    - [ ] Detect alias cycles (A→B→C→A)
    - [ ] Validate alias targets exist
    - [ ] Check for ambiguous aliases
  - [ ] Entity validation:
    - [ ] Detect entity conflicts (duplicate IDs)
    - [ ] Validate taxonomy cross-references
    - [ ] Check translation completeness
  - [ ] Output:
    - [ ] Errors (blocking issues)
    - [ ] Warnings (best practice violations)
    - [ ] Info (suggestions)
  - [ ] Exit code: 0 (valid), 1 (errors), 2 (warnings)

### Plugin Scaffolding Tool

- [PLAN] 📦 *ctxt* Implement `ctxt dev init-plugin` command
  - [ ] Interactive prompts:
    - [ ] Plugin name
    - [ ] Plugin type (pipeline, command, semantic_augmentor)
    - [ ] Language (Go, Python, TypeScript)
    - [ ] Required permissions
  - [ ] Generate scaffolding:
    - [ ] `manifest.yaml` - plugin metadata
    - [ ] Permission declarations:
      - [ ] Network access
      - [ ] Filesystem access
      - [ ] Vector storage access
      - [ ] Entity write access
    - [ ] Interface implementations:
      - [ ] Pipeline hooks (pre, execute, post)
      - [ ] Mention augmentation hooks
      - [ ] Entity enrichment functions
      - [ ] Graph extension points
    - [ ] Example code and tests
    - [ ] README with development guide
  - [ ] Validation:
    - [ ] Check generated code compiles
    - [ ] Run basic tests
    - [ ] Validate manifest against schema

### Documentation Generation

- [ ] 📦 *dPKMS* Auto-generate API documentation
  - [ ] Generate from Go interfaces and proto definitions:
    - [ ] `REGISTRY_SPEC.md` - registry API specification
    - [ ] `PLUGIN_API.md` - plugin development guide
    - [ ] `ENTITY_SCHEMA.md` - entity and mention schema
    - [ ] `GRAPH_API.md` - knowledge graph operations
  - [ ] Include:
    - [ ] Interface signatures
    - [ ] Field descriptions
    - [ ] Example requests/responses
    - [ ] Semantic identity rules
    - [ ] Namespace conventions
  - [ ] Tools:
    - [ ] godoc extraction
    - [ ] protoc-gen-doc for gRPC
    - [ ] Custom markdown generator
  - [ ] Publish to docs site automatically on release

### Multi-Registry Entity Reconciliation

- [PLAN] 📦 *dPKMS* Implement deterministic conflict resolution
  - [ ] Reconciliation rules:
    - [ ] **Priority order:** Local > selected registries (user order) > fallback registries
    - [ ] **Namespace boundaries:** Strict isolation, no cross-namespace aliases without explicit mapping
    - [ ] **Version conflicts:** Higher version wins, warn if non-sequential
    - [ ] **Alias conflicts:** First registry to define alias wins
  - [ ] Validation:
    - [ ] Aliases cannot cross namespace boundaries
    - [ ] Version numbers must increment (warn on gaps)
    - [ ] Entity IDs must be unique within namespace
  - [ ] Conflict logging:
    - [ ] Log file: `~/.config/contexthelp/registry-conflicts.log`
    - [ ] Include: timestamp, registries involved, conflict type, resolution
    - [ ] User notification on significant conflicts
  - [ ] **Deterministic resolution order:**
    - [ ] Same registries + same versions = same entities
    - [ ] Reproducible across machines
    - [ ] No non-deterministic hash ordering
  - [ ] Conflict types:
    - [ ] Duplicate entity IDs (namespace collision)
    - [ ] Alias collision (multiple entities claim same alias)
    - [ ] Version mismatch (entity versions out of order)
    - [ ] Namespace spoofing (registry defines entity in wrong namespace)

---

## Integration Check (Air-Gapped Test)

### Setup: Offline Environment

1. [ ] Configure system:
   ```yaml
   offline_mode: true
   ai_providers:
     ollama:
       enabled: true
       models:
         completion: llama2
         embedding: nomic-embed-text
   registries:
     - name: local-cache
       type: local
       path: ~/.config/contexthelp/registry-cache/
   ```

2. [ ] Prerequisites:
   - [ ] Machine in airplane mode (no network)
   - [ ] Ollama running locally
   - [ ] Local entity registry populated

### Scenario: Ingestion While Offline

1. [ ] Run:
   ```bash
   ctxt analyze --text "Deploying via Kubernetes using @devops.k8s"
   ```

2. [ ] Verify:
   - [ ] Local model extracts mentions: `@devops.k8s`
   - [ ] Resolver loads/creates local entity (no network call)
   - [ ] Graph updated (backlink created)
   - [ ] Embedding generated via Ollama
   - [ ] Job completes successfully
   - [ ] No network errors in logs

### Scenario: Search with Graph Expansion

1. [ ] Run:
   ```bash
   ctxt list --q "mention:devops.k8s" --expand-entities 2
   ```

2. [ ] Verify:
   - [ ] Hybrid search executes:
     - [ ] Vector similarity (local embeddings)
     - [ ] Graph-based filtering (backlinks)
   - [ ] No network calls attempted
   - [ ] Results include:
     - [ ] Direct mentions of `@devops.k8s`
     - [ ] 1-hop neighbors (objects sharing related entities)
     - [ ] 2-hop neighbors (extended graph)
   - [ ] Ranking stable and deterministic
   - [ ] Score breakdown includes graph proximity

### Validation Checklist

**dPKMS Validation:**
- [ ] Performance profiling identifies bottlenecks
- [ ] Binary distribution works across platforms
- [ ] Offline mode blocks all network requests
- [ ] Entity/graph storage isolation prevents corruption
- [ ] sqlite-vec or HNSW provides fast vector search
- [ ] Vector compression reduces storage size
- [ ] Multi-registry reconciliation is deterministic

**ctxt Validation:**
- [ ] Ollama client generates completions and embeddings
- [ ] Local fallback strategy works without network
- [ ] Prompts produce consistent output with small models
- [ ] Mention extraction stable across modalities
- [ ] Reindex command updates all vectors and graph
- [ ] Registry linter detects conflicts
- [ ] Plugin scaffolding generates working code

**Cross-Package Validation:**
- [ ] Offline ingestion persists to dPKMS storage
- [ ] Graph queries execute without network
- [ ] Entity resolution uses cached registries
- [ ] Vector search integrates with mention filters
- [ ] Ranking considers graph proximity

---

## Risks to Watch For

- **Local LLM Variance:** Hardware differences affect timeouts and memory
- **CGO Portability:** `sqlite-vec` may complicate builds on some platforms
- **Reindexing Scale:** Large databases need pause/resume support
- **Multi-Registry Conflicts:** Semantic drift if rules not enforced
- **Graph Index Pressure:** Heavy graph updates may slow ingestion
- **Offline Edge Cases:** Some pipelines may fail without network detection

---

## See Also

- Sprint spec: `docs/sprints/007-sovereign-and-scalable.md`
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
- Package placement: `docs/dpkms-or-ctxt.md`
