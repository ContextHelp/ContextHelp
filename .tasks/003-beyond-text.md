# Skeleton 3: Beyond Text Tasks

**Package Focus:** dPKMS (50%) + ctxt (50%)

**Goal:** Introduce semantic identity (mentions + entities + knowledge graph) and multimodal ingestion.

---

## dPKMS Infrastructure Team (The Foundation)

### SQLite FTS5 Migration (ADR-006)

- [PLAN] 📦 *dPKMS* Implement full-text search
  - [ ] Create virtual table `knowledge_objects_fts` using FTS5
  - [ ] Define indexed columns (title, raw content, sections)
  - [ ] Add triggers for automatic sync:
    - [ ] INSERT trigger on knowledge_objects
    - [ ] UPDATE trigger on knowledge_objects
    - [ ] DELETE trigger on knowledge_objects
  - [ ] Test FTS5 across platforms (macOS, Linux, Windows)
  - [ ] Document CGO and build flag requirements
  - [ ] Add migration script for existing databases

### Zombie Job Recovery (ADR-007)

- [ ] 📦 *dPKMS* Implement crash recovery on worker startup
  - [ ] On `dpkms serve` startup, run:
    ```sql
    UPDATE jobs
    SET status = 'pending',
        attempts = attempts + 1
    WHERE status = 'running';
    ```
  - [ ] Log recovered jobs for debugging
  - [ ] Add max attempts threshold (mark as Failed after N attempts)
  - [ ] Essential for long-running URL/OCR pipelines

### Mention + Entity Storage Integration

- [PLAN] 📦 *dPKMS* Extend storage layer for semantic identity
  - [ ] Update `WriteKnowledgeObject()` to persist:
    - [ ] `mentions[]` field (array of entity IDs)
    - [ ] `entities[]` metadata (optional denormalized cache)
  - [ ] Implement backlink creation in `AddBacklink()`:
    - [ ] Insert into `entity_backlinks` table
    - [ ] Handle duplicates gracefully (UPSERT or ignore)
  - [ ] Ensure entity IDs remain stable across ingestion cycles
  - [ ] Add migration to populate backlinks for existing data
  - [ ] Index entity_id and knowledge_object_id columns

### Job Step Logging

- [ ] 📦 *dPKMS* Implement `ctxt jobs logs <id>` command
  - [ ] Query `job_steps` table for given job ID
  - [ ] Display step-by-step execution:
    - [ ] Fetch (for URLs)
    - [ ] Markdown conversion
    - [ ] OCR (for images)
    - [ ] Mention extraction
    - [ ] Entity resolution
    - [ ] Graph updates (backlinks created)
  - [ ] Show discovered mentions and resolved entities
  - [ ] Display timing and status for each step

---

## ctxt Ingestion Team (The Heavy Lifters)

### URL Pipeline Implementation

- [PLAN] 📦 *ctxt* Create `url.generic` pipeline
  - [ ] Implement `HTMLFetcher`:
    - [ ] HTTP client with timeout
    - [ ] User-agent string
    - [ ] Handle redirects
    - [ ] Return HTML content
  - [ ] Implement `ReadabilityConverter`:
    - [ ] Extract main content from HTML
    - [ ] Convert to clean Markdown
    - [ ] Preserve metadata (title, author, date)
  - [ ] Pipeline chain:
    1. [ ] Fetch URL → HTML
    2. [ ] Convert HTML → Markdown
    3. [ ] Extract mentions from Markdown
    4. [ ] Resolve mentions → entities (registry + local placeholders)
    5. [ ] Generate backlinks in knowledge graph
    6. [ ] Summarize via `text.short`
  - [ ] Handle fetch failures gracefully
  - [ ] Log each step to job_steps

### Image Pipeline Implementation

- [PLAN] 📦 *ctxt* Create `image.ocr` pipeline
  - [ ] Define `OCRClient` interface:
    ```go
    type OCRClient interface {
        ExtractText(ctx context.Context, imageData []byte) (string, error)
    }
    ```
  - [ ] Implement local OCR adapter (Tesseract) or LLM-based OCR
  - [ ] Pipeline steps:
    1. [ ] Load image data
    2. [ ] Extract text via OCR
    3. [ ] Extract mentions from OCR text
    4. [ ] Resolve mentions → entities
    5. [ ] Update knowledge graph (backlinks)
    6. [ ] Summarize via `text.short`
  - [ ] Handle OCR errors (unreadable images, wrong format)
  - [ ] Log mention extraction quality metrics

### Modal Step Tracing

- [ ] 📦 *ctxt* Emit structured logs to dPKMS job_steps
  - [ ] Each pipeline step writes to job_steps table
  - [ ] Include structured output:
    ```
    Step 1: Fetching URL... success (234ms)
    Step 2: Converting to Markdown... success (89ms)
    Step 3: Extracting mentions... found 3 mentions (12ms)
    Step 4: Resolving entities... 2 registry hits, 1 local placeholder (145ms)
    Step 5: Creating backlinks... 3 edges added (23ms)
    ```
  - [ ] Surface errors with context
  - [ ] Enable debugging via `ctxt jobs logs <id>`

---

## dPKMS Search Team & ctxt Retrieval Team (The Reranker)

### Reranker Service (ADR-011)

- [PLAN] 📦 *dPKMS* Implement unified search result merging
  - [ ] Define `SearchResult` struct:
    ```go
    type SearchResult struct {
        KnowledgeObject domain.KnowledgeObject
        Score           float64
        Source          string // "local", "registry:name"
        MatchType       string // "fts", "mention", "entity"
    }
    ```
  - [ ] Implement deduplication logic:
    - [ ] Knowledge objects referencing same entity merge into one result
    - [ ] Local results outrank registry results
    - [ ] Entity-aware scoring: direct mention > inferred via backlinks
  - [ ] Ranking formula considers:
    - [ ] FTS relevance score
    - [ ] Mention match (exact vs partial)
    - [ ] Entity alignment
    - [ ] Recency
    - [ ] Registry weights

### FTS + Mention Query Integration

- [PLAN] 📦 *dPKMS* Enhance query execution
  - [ ] AST-to-SQL generation must:
    - [ ] Route text clauses to FTS5
    - [ ] Translate `mention:` filters to:
      - [ ] Exact match on mentions JSON field
      - [ ] JOIN against `entity_backlinks` table
    - [ ] Support hybrid queries: `"design" AND mention:ui.best-practice`
  - [ ] Query planner optimizations:
    - [ ] Avoid exponential JOIN behavior
    - [ ] Use covering indexes where possible
    - [ ] Limit backlink expansion depth
  - [ ] Add query performance logging

---

## dPKMS Registry Team (The Provider)

### Remote Bookmark Search Client

- [PLAN] 📦 *dPKMS* Implement federated search
  - [ ] Fetch knowledge objects from registry endpoints
  - [ ] Normalize remote results to local format
  - [ ] Parse mentions and resolved entity IDs from registry
  - [ ] Ensure registry entity IDs are namespace-safe
  - [ ] Validate entity provenance (registry source)

### Parallel Scatter/Gather with Timeouts

- [PLAN] 📦 *dPKMS* Implement multi-source query execution
  - [ ] Query local database + all configured registries simultaneously
  - [ ] Use goroutines with timeout context
  - [ ] Enforce per-registry timeout budget (e.g., 500ms)
  - [ ] Merge results:
    - [ ] Local knowledge objects
    - [ ] Registry knowledge objects
    - [ ] Entity-linked relationships
  - [ ] Deduplication by entity identity (not just object ID)
  - [ ] Ranking: local > registry priority order
  - [ ] Return unified result set

---

## Integration Check (Multi-Modal Demo)

### Scenario 1: URL Ingestion with Mentions

1. [ ] Run:
   ```bash
   ctxt analyze --type url https://example.com/design-patterns
   ```

2. [ ] Verify:
   - [ ] HTML fetched successfully
   - [ ] Converted to Markdown
   - [ ] Mentions extracted: `@design.pattern.strategy`, `@oop.principle`
   - [ ] Entities resolved (2 registry hits)
   - [ ] Backlinks created in graph
   - [ ] Job logs show all steps

### Scenario 2: Image Ingestion with OCR

1. [ ] Run:
   ```bash
   ctxt analyze --file screenshot.png
   ```

2. [ ] Verify:
   - [ ] OCR extracts text: "Follow @ui.best-practice guidelines"
   - [ ] Mention `@ui.best-practice` detected
   - [ ] Entity resolved from registry
   - [ ] Knowledge object enriched with entity metadata
   - [ ] Graph updated with backlink

### Scenario 3: Mention-Aware Search

1. [ ] Run:
   ```bash
   ctxt list --q 'mention:ui.best-practice "pattern"'
   ```

2. [ ] Verify:
   - [ ] FTS finds "pattern" in text
   - [ ] Entity lookup matches `ui.best-practice` locally
   - [ ] Registry returns UX patterns referencing same entity
   - [ ] Reranker merges results by entity identity
   - [ ] Results ranked: local > registry
   - [ ] Duplicate objects merged

### Validation Checklist

**dPKMS Validation:**
- [ ] FTS5 virtual table and triggers functional
- [ ] Zombie job recovery works on startup
- [ ] Entity and backlink storage persists correctly
- [ ] Job step logging captures pipeline execution
- [ ] Mention-aware queries execute without errors
- [ ] Reranker deduplicates by entity identity

**ctxt Validation:**
- [ ] URL pipeline fetches and converts HTML
- [ ] Image pipeline performs OCR
- [ ] Mention extraction works across modalities
- [ ] Entity resolution uses registry + local fallback
- [ ] Backlinks created correctly in graph

**Cross-Package Validation:**
- [ ] ctxt pipelines write mentions and entities to dPKMS
- [ ] dPKMS query engine supports mention filters
- [ ] Reranker merges local + registry results
- [ ] Graph traversal via backlinks functional

---

## Risks to Watch For

- **FTS + CGO Portability:** Build reliability across macOS/Linux required
- **HTML Parsing Failures:** URL pipeline must handle malformed HTML
- **OCR Inaccuracies:** Mention extraction must tolerate noisy OCR text
- **Entity Drift:** Registry and local entity definitions may diverge—stable namespaces critical
- **Search Performance:** Mention + graph JOINs must remain performant under large graphs
- **Backlink Explosion:** Entities with many inbound references must not degrade query latency

---

## See Also

- Sprint spec: `docs/sprints/003-beyont-text.md`
- ADR-006: FTS5 implementation
- ADR-007: Job recovery strategy
- ADR-011: Reranking and deduplication
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
