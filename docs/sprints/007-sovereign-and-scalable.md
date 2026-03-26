# Skeleton 7 Goal: "Sovereignty & Scale"

## Package Focus

**Balanced:** dPKMS (50%) + ctxt (50%)

Sovereignty requires offline AI (ctxt) and scalable infrastructure (dPKMS) working in harmony.

**Package Breakdown:**
- **dPKMS:** Performance profiling, vector optimization, entity/graph isolation, offline mode enforcement
- **ctxt:** Ollama integration, local model support, mention extraction stability, developer tooling

---

By the end of this skeleton:

1. **True Offline AI:** Users can connect Ollama or LocalAI to run pipelines without an internet connection.
2. **Vector Scalability:** Retrieval migrates from brute-force scans to optimized vector storage (`sqlite-vec` or HNSW), enabling 100k+ knowledge objects and large entity graphs.
3. **Developer Experience (DX):** Tooling is released to help third parties build Plugins and Registries without touching core.
4. **Semantic Sovereignty:** Entity resolution, namespace governance, alias handling, and graph stability remain deterministic across offline, mixed-registry, and multi-plugin environments.

---

## 1. dPKMS Infrastructure Team (The Optimizer)

Focus: Performance tuning, offline governance, graph stability, and predictable execution even under heavy semantic workloads.
Why: The addition of entities, mentions, backlinks, and registry-driven semantics requires stricter guarantees on write paths, graph integrity, and resolution logic.

### Task 1.1: Performance Profiling (`pprof`)

- Profile ingestion with:
  - 1,000+ URLs
  - multimodal content
  - mention extraction + entity resolution enabled
- Optimize:
  - WAL checkpoint cadence
  - mutex contention during entity/backlink writes
  - pipeline worker goroutine pressure
- Ensure entity index updates do not stall the ingestion pipeline.

### Task 1.2: Binary Distribution (GoReleaser)

- CI publishes signed binaries for:
  - macOS Universal
  - Linux (AMD64/ARM64)
  - Windows
- Deliverable: Homebrew formula, manual downloads, and checksum table.

### Task 1.3: The "Strict Offline" Flag

Implement `ctxt config --offline`.

Behavior:

- Hard-block all outbound network requests.
- Registries do not sync.
- Plugins requiring networking must be denied unless explicitly permitted.
- Mention resolution:
  - Only local entity registry is used.
  - Unresolved mentions → stub entities (local namespace).
- Pipelines must degrade gracefully (skip remote steps).

### Task 1.4: Entity/Graph Storage Isolation

- Add fine-grained write locks for:
  - entity table
  - alias table
  - backlinks table
- Ensure graph updates remain amortized, not O(n).
- Guarantee referential integrity even during worker crashes.

---

## 2. ctxt Ingestion Team (The Localist)

Focus: Local models, mention extraction, entity resolution, and stable semantics in offline or constrained environments.
Why: Semantic extraction must not depend on cloud models.

### Task 2.1: `OllamaClient` Implementation

- Implement both `LLMClient` and `EmbeddingClient` interfaces.
- Allow per-pipeline model mapping and fallback order.

### Task 2.2: Local Fallback Strategy

- Resolution order:
  Cloud → Local → Rule-based extraction → Raw text heuristics.
- Mention extraction runs regardless of model availability.

### Task 2.3: Prompt Stability for Small Models

Ensure deterministic extraction of:

- summaries
- sections
- decisions
- tags
- hints
- mentions (`@namespace.id`)

Across:

- text
- OCR
- transcripts
- metadata

### Task 2.4: Mention Extraction Stability

- Enforce strict mention syntax rules.
- Surface structured errors for malformed mentions.
- Add detection heuristics for false positives.

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Architect)

Focus: Hybrid retrieval combining vectors, graph traversal, and semantic ranking.
Why: Queries involving `mention:` or entity expansions require integrating graph neighbors and embeddings.

### Task 3.1: `sqlite-vec` Integration

- Integrate `sqlite-vec` if CGO is acceptable.
- Fallback: pure-Go HNSW index with disk persistence.

### Task 3.2: Vector Compression

- Introduce quantization (int8 or binary) for embeddings.
- Ensure plugin-provided embedding backends can plug in seamlessly.

### Task 3.3: Re-indexing Command

Implement:

```
ch jobs reindex-vectors
```

Tasks:

- Recompute embeddings.
- Update entity aggregate vectors.
- Rebuild graph-derived similarity fields.

### Task 3.4: Mention- and Entity-Aware Ranking

Ranking enhancements:

- Boost knowledge objects referencing the queried entity.
- Graph expansion:
  - via backlinks
  - via alias-linked entities
  - via registry-derived relations
- `--expand-entities` expands search radius.

---

## 4. dPKMS Registry Team (The Enabler)

Focus: Tooling for building registries and plugins + deterministic entity governance across multiple registries.
Why: Registries now define taxonomies, entities, aliases, and metadata that drive semantic identity.

### Task 4.1: Registry Linter (`ctxt dev validate-registry`)

Validate:

- JSON schemas
- namespace rules
- alias cycles
- version fields
- entity conflicts
- taxonomy cross-references
- reserved ID prefixes

### Task 4.2: Plugin Scaffolding (`ctxt dev init-plugin`)

Generate scaffolding that includes:

- metadata block
- permission block (network/fs/vector access)
- pipeline hooks
- mention augmentation hooks
- entity enrichment functions
- graph extension points

### Task 4.3: Documentation Generation

Auto-generate:

- `REGISTRY_SPEC.md`
- `PLUGIN_API.md`
- entity schema docs
- mention schema docs

From Go interfaces and proto definitions.

### Task 4.4: Multi-Registry Entity Reconciliation

Rules:

- Priority: Local > selected registry > fallback registry
- Namespace boundaries remain strict
- Aliases cannot cross namespaces without explicit mapping
- Version mismatches must surface warnings
- Conflicts produce deterministic, reproducible resolution order

---

## 5. Sovereignty & Scale: Semantic Extensions

Focus: Ensuring determinism, stability, and safety across decentralized semantics.
Why: Semantic drift must be impossible without user intent.

### Task 5.1: Entity Identity Guarantees

- IDs must remain stable across syncs.
- Registry updates cannot silently change meaning.
- Stub entities must upgrade deterministically when registry definitions appear.

### Task 5.2: Backlink Index at Scale

Optimize:

- knowledge_object → entity edges
- entity → entity inferred edges
- query-time graph expansion

Ensure:

- ingestion throughput unaffected
- graph updates amortized
- plugin extensions cannot corrupt graph state

### Task 5.3: Offline-First Resolution Rules

When offline:

- Mentions resolve only locally.
- Unknown entities create local stubs.
- Registry-sync jobs are queued but not executed.

When back online:

- Deferred resolution logic updates stubs.
- Alias table reconciles with remote definitions.

### Task 5.4: Conflict Resolution

Handle:

- ambiguous definitions
- alias collisions
- namespace spoofing
- version conflicts
- multi-registry disagreement

Conflicts must:

- be logged
- be visible to users
- not break ingestion
- not merge entities unpredictably

---

## The Integration Check (The Demo)

### Scenario: The "Air-Gapped" Test

1. Setup:
   - Machine offline
   - Ollama running
   - `offline: true`
   - Local entity registry

2. Ingest:
   ```
   ch analyze --text "Deploying via Kubernetes using @devops.k8s"
   ```
   - Local model extracts mentions
   - Resolver loads/creates local entity
   - Graph updated

3. Search:
   ```
   ch list --q "mention:devops.k8s"
   ```
   - Hybrid search uses:
     - vector similarity
     - graph-based filtering
   - No network calls

4. Validation:
   - Backlinks correct
   - Entity stable
   - Graph navigation functional offline
   - Ranking stable

---

## Task 6: User Productivity

### Task 6.2: Lightweight Reminders on Knowledge Objects (T-0131)

Add per-object reminders surfaced as desktop notifications from `dpkms serve`.

Schema:
- `remind_at DATETIME` — when to notify (nullable)
- `reminded_at DATETIME` — set after notification sent (nullable)

CLI:
- `ctxt remind <id> <time-expr>` — set reminder; supports "tomorrow 9am",
  "in 2h", "2026-04-01 10:00", weekday names
- `ctxt remind --clear <id>` — remove reminder
- `ctxt reminders` — list all pending reminders

Serve:
- `dpkms serve --reminder-interval <dur>` (default 1m) starts a goroutine
  polling `remind_at <= now AND reminded_at IS NULL`
- macOS: `osascript` display notification
- Linux: `notify-send`
- After delivery: sets `reminded_at`

---

## Risks to Watch For

- Local LLM hardware variance (timeouts, memory).
- CGO issues with `sqlite-vec` reducing portability.
- Large-scale reindexing operations requiring pause/resume.
- Multi-registry conflicts causing semantic drift.
- Graph index pressure slowing ingestion or resolution.
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
