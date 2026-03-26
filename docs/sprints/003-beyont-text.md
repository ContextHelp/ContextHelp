# Skeleton 3 Goal: "Beyond Text: Modalities, Mentions & Merging"

## Package Focus

**Balanced:** dPKMS (50%) + ctxt (50%)

This skeleton introduces semantic identity (mentions + entities + graph). ctxt extracts mentions from multimodal content, dPKMS resolves entities and maintains the knowledge graph.

**Package Breakdown:**
- **dPKMS:** FTS5 migration, entity storage, backlink index, graph queries, job recovery
- **ctxt:** URL pipeline, image/OCR pipeline, mention extraction, entity resolution integration

---

By the end of this skeleton, a user should be able to:

1. Ingest a **URL**, have the engine fetch the HTML, convert it to Markdown, extract mentions, resolve entities, populate backlinks, and summarize it.
2. Ingest an **Image**, extract text via OCR, detect mentions within the OCR output, resolve them to canonical entities (registry or local placeholders), and enrich the resulting bookmark.
3. Perform a search using text and/or a mention (e.g., `@ui.best-practice`) and see mixed **local + registry** results, merged and deduped by entity identity through the knowledge graph subsystem.

---

## 1. dPKMS Infrastructure Team (The Foundation)

**Focus:** Full-Text Search (FTS), Job Resilience, and Mention-Aware + Entity-Aware Storage.

**Why:**
Search must now support:
- pure text
- tag filters
- **mention filters** → entity lookups → graph edge traversal

To enable this, storage must index mentions, store resolved entities, and maintain backlinks for the knowledge graph.

### Task 1.1: SQLite FTS5 Migration (ADR-006)

- Create a Virtual Table `bookmarks_fts` using SQLite FTS5.
- Add triggers syncing FTS rows on `INSERT`, `UPDATE`, `DELETE`.
- Ensure that all textual material (HTML→Markdown, OCR text, transcripts) is indexed.
- Confirm CGO and build flags are compatible with FTS5 on all platforms.

### Task 1.2: Zombie Job Recovery (ADR-007)

- On worker startup:
  ```
  UPDATE jobs
  SET status = 'pending',
      attempts = attempts + 1
  WHERE status = 'running';
  ```
- Essential for long-running pipelines such as OCR or remote HTML fetches.

### Task 1.3: Mention + Entity Storage Integration

- Add `mentions[]` and `entities[]` fields to knowledge object persistence.
- Add `bookmark_entity_edges` table for backlinks.
- Ensure entity IDs remain stable across ingestion cycles.
- Migration scripts must populate backlinks when mentions resolve.

### Task 1.4: `ctxt job log <id>`

- Expose pipeline execution step-by-step:
  - fetch
  - Markdown conversion
  - OCR
  - mention extraction
  - entity resolution
  - graph updates
- Logs must explicitly show discovered mentions, resolved entities, and backlink creation.

---

## 2. ctxt Ingestion Team (The Heavy Lifters)

**Focus:** URL & Image Pipelines + Mention Extraction + Entity Resolution + Knowledge Graph Integration.

**Constraint:**
Every modality must support:
- mention extraction
- entity resolution (registry + local placeholder)
- edge creation in the knowledge graph

### Task 2.0: `ctxt inbox` Command

- Implement `ctxt inbox` to show all knowledge objects in a **raw or pending state**.
- "Pending" = job exists but has not reached `completed` status.
- "Raw" = object was ingested with `--raw` flag (no enrichment yet; see Skeleton 4 `url.repo` gap).
- Output: table with columns `ID | Type | Status | Title | Created`.
- Filter flags:
  - `--pending` — show only objects awaiting pipeline completion
  - `--failed` — show only objects whose job failed
  - `--raw` — show only unenriched raw objects
- Default (no flag): show all in-progress or failed items (the "needs attention" inbox).
- Implementation note: queries both `jobs` and `knowledge_objects` tables; left-joins on `object_id`.

### Task 2.1: `url.generic` Pipeline

- Implement `HTMLFetcher`.
- Implement `ReadabilityConverter` (HTML → Markdown).
- Pass Markdown through:
  - Mention Extraction
  - Entity Resolution
  - Backlink Generation
  - Summarization (`text.short`)
- Pipeline chain:
  `Fetch → Markdown → Mentions → Entities → Graph → Short Summary`

### Task 2.2: `image.ocr` Pipeline

- Define `OCRClient` interface (local OCR engine or LLM-based).
- OCR output becomes the canonical text input.
- Pipeline steps:
  - Mention Extraction
  - Entity Resolution
  - Graph Updates (entity backlinks)
  - Summarization via `text.short`

### Task 2.3: Modal Step Tracing

- Each step must emit structured logs into `job_steps`.
- Example:
  ```
  Step 3: Resolving mentions... 2 mentions, 2 entities resolved (1 local placeholder).
  ```

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Reranker)

**Focus:** Mention-Aware + Entity-Aware Search, Reranking, and Multi-Source Merge (ADR-011).

**Why:**
Queries now operate at three semantic layers:
- text search
- tag search
- **mention/entity search** (knowledge graph powered)

### Task 3.1: The `Reranker` Service

- Define a unified `SearchResult` struct across local and remote searches.
- Enhance dedupe logic:
  - knowledge objects referencing the same entity should merge into one logical result
  - local results outrank registry ones (Local > Registry)
- Add entity-aware scoring:
  - direct mention match > inferred match via backlinks

### Task 3.2: FTS + Mention Query Integration

- AST-to-SQL generation must:
  - route textual clauses to FTS
  - translate `mention:` filters into:
    - exact match on mentions field
    - or join against `bookmark_entity_edges`
- Support hybrid queries like:
  ```
  "design" AND mention:ui.best-practice
  ```
- Ensure the query planner avoids exponential join behavior.

---

## 4. dPKMS Registry Team (The Provider)

**Focus:** Multi-Source Mention + Entity Semantics.

**Why:**
Registries now return:
- canonical entities
- aliases
- definitions
- entity-linked bookmarks

Remote results must integrate seamlessly into local entity graphs.

### Task 4.1: Remote Bookmark Search Client

- Implement fetch + normalize logic to ingest:
  - mentions
  - resolved entity IDs from registry
- Registry-provided entity IDs must be namespace-safe and deterministic.

### Task 4.2: Parallel Scatter/Gather with Timeouts

- Query local + all registries simultaneously.
- Enforce per-registry timeout budget.
- Merge:
  - local bookmarks
  - registry bookmarks
  - entity-linked relationships
- Dedup by entity identity, not just by knowledge object ID.

---

## The Integration Check (The Demo)

**Scenario:** The Multi-Modal, Multi-Source, Mention-Aware End-to-End Demonstration.

1. **Ingest URL:**
   ```
   ch analyze --type url https://example.com/design-patterns
   ```
   - HTML → Markdown
   - mentions: `@design.pattern.strategy`, `@oop.principle`
   - entities resolved (2 registry hits)
   - backlinks created

2. **Ingest Image:**
   ```
   ch analyze --file screenshot.png
   ```
   - OCR extracts: “Follow @ui.best-practice guidelines”
   - mention resolved
   - knowledge object enriched; graph updated

3. **Search:**
   ```
   ch list --q 'mention:ui.best-practice "pattern"'
   ```
   - FTS finds “pattern” text
   - entity lookup matches `ui.best-practice` locally
   - registry returns UX patterns referencing the same entity
   - Reranker merges results into consistent semantic groups

---

## Risks to Watch For

- **FTS + CGO portability:** Build reliability across macOS/Linux.
- **HTML parsing failures:** URL pipeline variance must be captured in logs.
- **OCR inaccuracies:** Mention extraction must tolerate noisy text.
- **Entity drift:** Registry and local entity definitions may diverge—stable namespaces required.
- **Search performance:** Mention + graph joins must remain performant under large graphs.
- **Backlink explosion:** Entity with high inbound references must not degrade query latency.
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
