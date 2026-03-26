# Skeleton 5 Goal: "Semantics & Sidecars"

## Package Focus

**Balanced:** dPKMS (50%) + ctxt (50%)

Semantic search requires both packages equally: ctxt generates embeddings and defines plugins, dPKMS stores vectors and enables hybrid retrieval.

**Package Breakdown:**
- **dPKMS:** Plugin loader, vector storage, RRF implementation, entity graph integration
- **ctxt:** Embedding client, pipeline vector integration, semantic augmentation, plugin definitions

---

By the end of this skeleton:

1. **Semantic Search:** Users can search for concepts (e.g., "fruit") and find relevant knowledge objects (e.g., "apple") even if the literal keyword does not appear, using entity-driven semantics.
2. **Plugins:** A developer can write a separate Go binary that ContextHelp loads to add a custom command, pipeline step, or semantic augmentation sidecar.
3. **Hybrid Reranking:** Search results combine Keyword (FTS), Vector (Embeddings), and Mention/Entity alignment through Reciprocal Rank Fusion (RRF).
4. **Semantic Identity Integration:** Pipelines and plugins must support mentions (`@concept.id`) and entity resolution. Plugins can safely contribute suggestions, aliases, or metadata to the knowledge graph without modifying core identity rules.

---

## 1. dPKMS Infrastructure Team (The Host)

Focus: The Plugin System (ADR-012).

New Responsibility: Ensure plugins can safely participate in semantic enrichment, producing mentions or entity metadata while respecting core ownership of canonical entities, storage invariants, and namespace integrity.

### Task 1.1: Plugin Interface Definitions

Define interfaces for:

- `PluginPipelineStep`
- `PluginCommand`
- `PluginSemanticAugmentor` (new)

Contract requirements:

- Plugins may emit:
  - mentions (`@concept.id`)
  - local entity definitions (namespaced)
  - alias suggestions
  - metadata enrichments for known entities
- Plugins must treat:
  - hints, tags, and mentions as independent domains
- Plugins must not:
  - overwrite canonical entity IDs
  - mutate registry-provided definitions
- Plugins may:
  - create namespaced local entities (`plugin.myplugin.entityName`)
  - propose backlinks and semantic relationships

### Task 1.2: The Plugin Loader

Responsibilities:

- Discover plugin binaries in `~/.config/contexthelp/plugins/`.
- Load into isolated process or RPC mode as defined by ADR-012.
- Provide semantic-safe boundaries:
  - Plugins cannot override existing entity records.
  - Plugins cannot rename or delete canonical entities.
  - Plugins cannot modify mention or entity schema tables directly.
- Safely allow:
  - local entity creation
  - sidecar-based semantic enrichment through defined interfaces
  - plugin-provided graph edges stored in plugin namespaces

### Task 1.3: Polymorphic Config Update

Enhancements:

- Route config blocks to relevant plugin types.
- Add support for:
  - entity-sidecar configuration (`semanticAugmentor:` namespace)
  - plugin-controlled semantic augmentation toggle
  - per-plugin namespace policies (which entity namespaces they may emit)
  - limits on emitted mentions or entity nodes

---

## 2. ctxt Ingestion Team (The Embedder)

Focus: Vector Generation, Mention Extraction, Entity Resolution.

New Responsibility: Semantic identity (mentions and resolved entities) must be determined before vectors are generated, ensuring stable entity-aware embedding representation.

### Task 2.0: `url.repo` Pipeline

- Implement a **Git repository-aware** URL pipeline triggered when a URL matches `github.com/*/*` (or any hosted git forge pattern).
- Detection: `url.repo` detector runs before `url.generic`; if pattern matches, use this pipeline.
- Pipeline steps:
  1. Fetch repository metadata (README, description, topics/tags, license, language, stars) via public API (no auth required for public repos).
  2. Extract mentions from README text.
  3. Resolve mentions to entities.
  4. Generate a short summary using `text.short`.
  5. Store with type `url.repo` for later filtering.
- Config keys:
  ```yaml
  pipelines:
    url.repo:
      enabled: true
      fetch_readme: true
      max_readme_chars: 8000
  ```
- Fallback: if API fetch fails (rate limit, private repo), fall back to `url.generic` and log the downgrade.

### Task 2.1: Raw Mode Ingestion (`--raw` flag)

- Add `--raw` flag to `ctxt analyze` (and all ingestion entry points).
- Behaviour when `--raw` is set:
  - Skip all AI enrichment steps (no LLM calls, no embedding generation, no mention extraction).
  - Store the object immediately in a `raw` state with only the source content preserved.
  - Mark the job as `completed` with a `raw=true` metadata field.
  - Object appears in `ctxt inbox --raw`.
- Useful for: bulk imports, offline capture, cost control.
- Enrichment can be triggered later with `ctxt enrich <id>` (stub in this sprint; full implementation in Sprint 007).
- Config default:
  ```yaml
  ingestion:
    default_raw: false  # set true to make raw the default globally
  ```

### Task 2.2: `EmbeddingClient` Interface

Implement:

- Adapter for OpenAI, local ONNX, or other providers
- Caching decorator for deterministic embeddings
- Guarantee ordering:
  1. Mention extraction
  2. Entity resolution
  3. Chunking
  4. Embedding

### Task 2.3: Pipeline Update (`text.long`, `url.generic`)

New processing order:

1. Extract mentions (`@namespace.id`) from raw or preprocessed text.
2. Resolve each mention into an entity:
   - registry → local → plugin-defined namespaces
3. Allow semantic sidecars (plugins) to:
   - propose additional mentions
   - suggest alias relationships
   - attach metadata to local entities
4. Chunk long text consistently.
5. Generate embeddings per chunk.
6. Attach:
   - mentions
   - resolved entity IDs
   - vectors
   - backlink updates

### Task 2.4: Storage Schema Update

Add or refine:

- `vectors` table for embedding storage
- `mentions[]` array in knowledge_object schema
- `entity_index` table
- `backlinks` table (initial implementation for graph edges)

Store vectors as:

- JSON arrays or
- Binary blobs (configurable)

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Mathematician)

Focus: Vector Search, Entity Filtering, and Advanced Reranking.

New Responsibility: Integrate mention and entity semantics into relevance scoring and filtering.

### Task 3.1: Vector Search Implementation

Implementation notes:

- In-memory cosine similarity baseline
- Ensure mention/entity filters apply before vector scoring
- Respect query operators for:
  - positive mention filters
  - negative mention filters
  - wildcard namespaces

### Task 3.2: RSQL Extension

Support:

- `mention:"ui.best-practice"`
- `mention:stripe.api.*`
- `NOT mention:deprecated.entity`

Behavior:

- Extract query-time mentions
- Resolve query mention → canonical entity
- Retrieve linked knowledge objects via backlinks and mention table
- Combine with FTS and vector results

### Task 3.3: Reciprocal Rank Fusion (RRF)

Inputs to RRF now include:

- FTS Rank
- Vector Rank
- Mention/Entity Rank
- Optional registry-provided weight multipliers

Base formula:

Score = `1 / (k + FTS_Rank) + 1 / (k + Vector_Rank)`

Extend with:

- `+ mentionBoost` when the knowledge object contains an explicit mention that maps to the target entity
- `+ entityAffinityScore` based on graph adjacency

---

## 4. dPKMS Registry Team (The Polisher)

Focus: Weighting, Internationalization, and Entity Registries.

New Responsibility: Support entity-driven semantics, namespace resolution, and merging rules across registries.

### Task 4.1: Weight Application

Registry-provided weights influence:

- tag scoring
- entity scoring
- semantic reranking when entities are involved

Apply weight rules per:

- registry priority
- namespace isolation
- alias relationships

### Task 4.2: I18N Schema Support

Implement:

- `translations` field in bookmarks
- `translations` field in entity definitions
- RegistryClient must return localized labels for tags and entities
- Preferred language affects display, not mention syntax

Names remain canonical regardless of language.

### Task 4.3: Entity Syncing

Registry endpoints may provide:

- entity definitions
- alias lists
- merged metadata
- entity versions

Merging strategy:

- Local entity overrides remote if conflict
- Registry priority determines tie-breaking
- Canonical IDs remain stable
- Namespaced IDs avoid collisions

---

## Integration Check (The Demo)

Scenario: "Semantic Plugin + Mentions + Vector" Test.

### Step 1: Setup

User installs:

- Slack Notifier plugin
- Domain Concepts plugin (adds local entities, emits mentions)

Example config:

```
plugins:
  slack: { webhook: "..." }
  domainConcepts: { enabled: true }
```

### Step 2: Ingest

Command:

```
ch analyze --text "Our checkout flow needs to meet Stripe API best practices."
```

Pipeline steps:

1. Detect mentions: `@stripe.api.checkout`, `@ui.best-practice`
2. Resolve entities:
   - registry-provided canonical IDs
3. Plugin semantic augmentor:
   - add domain-specific mentions
   - provide alias proposals
4. Embeddings generated
5. Bookmark stored with:
   - mentions
   - entity IDs
   - backlinks

### Step 3: Search

```
ch list --q 'mention:stripe.api.*'
```

Behavior:

- Mention filter yields direct match
- Graph traversal may elevate related bookmarks
- Vector search provides semantic reinforcement

Result: Bookmark ranked at top due to explicit entity alignment.

---

## Risks to Watch For

### Plugin Risks

- Plugins must not override canonical entity IDs.
- Plugins may inadvertently emit too many mentions.
- Sidecar entity proposals may create namespace clutter.

### Vector + Entity Interactions

- Reranking may overweight entity proximity unless balanced.
- Need to detect circular alias relationships.
- Unresolved mentions must not pollute the graph.

### Ingestion Risks

- Early mention extraction may cause unnecessary resolution calls.
- Plugin-based augmentation must be bounded for performance.

### Graph Integrity

- Backlink creation must validate entity existence.
- Plugin-generated edges must respect namespace constraints.
- Corruption risks increase if plugins emit malformed edges.
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
