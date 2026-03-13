# Persona: Agents, LLMs, Tools

**Primary Role:** Autonomous systems (agentic frameworks, scheduled jobs, external tools, AI assistants)

---

## Goals

- Query knowledge base deterministically and reproducibly for reliable decision-making
- Extract and structure content with hard guarantees (no post-hoc validation needed)
- Rank and rerank results efficiently with minimal latency overhead
- Compose briefs, plans, and drafts from structured knowledge with traceable provenance
- Integrate seamlessly into workflows without manual intervention

---

## Interaction Pattern

### Query Path

#### Structured Mode (Recommended)
- Fetch `/query-schema` endpoint on startup to discover queryable properties, operators, and examples
- Construct explicit RSQL queries: `type==article;tags=in=recommended;created_at>2025-01-01`
- Benefits: deterministic results, cacheable, auditable, reproducible across runs
- Example agent flow:
  1. Receive `query-schema` with properties: `type`, `tags`, `created_at`, `pipeline`, `source`
  2. Receive operator examples: `==`, `!=`, `<`, `>`, `=in=`, `;` (AND), `,` (OR)
  3. Construct RSQL based on reasoning: "find articles tagged 'recommended' from last month"
  4. Execute deterministically, cache results with query as key

#### Natural Language Mode (Fallback)
- Ask in prose: `"show me recent recommended articles"`
- System translates via NLQ normalizer → multi-strategy execution
- Less reproducible (query might change, intent classification might fail)
- Useful for ad-hoc refinement or clarification follow-ups
- System logs both natural intent and execution query for transparency

**Agent Query Best Practice:**
```
1. Receive /query-schema on startup
2. For deterministic queries: construct RSQL
3. For refinement/clarification: use NLQ fallback
4. Log both intent (why?) and execution query (what?) separately
5. Cache results keyed by RSQL + profile context
6. Retry failed queries with explicit error handling
```

### Enrichment Path

#### Constrained Extraction
- Execute enrichment steps that **guarantee structured output** (no parsing errors)
- Write intent, entities, decisions as JSON directly to knowledge objects
- Leverage LMQL (local models) for **token-level logit masking** (hard constraints)
- Leverage `instructor` (API models) for **Pydantic validation with automatic retry**
- Leverage `outlines` (local models) for **regex/schema-guided sampling**

#### Enrichment Step Types
- **Tag Assignment** — constrained to registry vocabulary set (validated enum)
- **Mention/Entity Extraction** — regex-constrained to `@namespace.slug` format
- **Decision/Task Extraction** — dataclass with typed fields (impact: LOW/MEDIUM/HIGH, status: OPEN/RESOLVED/SUPERSEDED)
- **Pipeline Selector at Ingestion** — closed-set constraint over registered pipeline names
- **Inline Branching** — conditional extraction based on intermediate model decision (no extra round-trip)

### Composition Path

#### Programmatic Assembly
```
1. Query knowledge base for object IDs: type==decision;tags=in=urgent
2. Retrieve objects + graph neighbors (entities, mentions, related decisions)
3. Assemble atomic nodes following entity relationships
4. Apply composition template (brief, plan, draft)
5. Generate output with provenance metadata (which objects, which registries)
6. Return traceable, verifiable composition to user/system
```

**Example:**
- Agent query returns 3 decision objects
- System retrieves: associated entities (stakeholders, systems), mentions (who raised it), related decisions (prior context)
- Agent applies "decision brief" template
- Output includes which objects contributed to each section + confidence scores

---

## Key Pain Points

- **Schema Drift:** Knowledge object structure changes break parsing logic
- **Intent Classification Errors:** NLQ normalizer misclassifies intent → wrong strategy selected → irrelevant results
- **Rate Limiting:** Federated registry queries hit rate limits, slowing down searches
- **Cold Start:** Agents need `/query-schema` + intent examples on bootstrap to avoid guessing
- **Result Merging:** Multi-strategy results (graph + vector + FTS + metadata) may conflict; reranking may change order unexpectedly
- **Constraint Enforcement Fallback:** API-hosted models degrade to prompt-engineering (no hard guarantee)

---

## System Leverage

### Deterministic Query API
- RSQL guarantees same query always produces same result order
- Query Plan is explicit (what strategies will execute, in what order)
- Results include `rank.explain` payload (contributing factors, weighted components, source weighting)
- Auditable for compliance and debugging

### Constrained Extraction
- LMQL (local models) enforces hard constraints at token level (impossible to violate)
- `instructor` (API models) validates output against Pydantic schema, retries on failure
- `outlines` (local models) guides sampling with regex/JSON schema constraints
- Zero post-hoc validation needed; output is always parseable

### Distributed Tracing
- Agent intent logged separately from execution query
- Job state includes retry count, timestamps, error messages
- Deterministic replay: re-run same query with different AI provider or config

### Registry Weighting
- Registries supply: default search weight, domain specificity score, trust/credibility metadata
- Helps agents decide whether to include remote sources in search

### Multi-Strategy Execution
- Metadata Strategy: SQL queries on object properties (fast, deterministic)
- FTS Strategy: SQLite FTS5 on full-text content (keyword/phrase matching)
- Vector Strategy: Optional semantic similarity search
- Graph Strategy: Entity-aware traversal (relationship discovery)
- Registry Strategy: Federated `/search` queries (knowledge from external sources)
- Results merged + reranked using configurable algorithms (RRF, weighted sum, softmax)

---

## User Stories

Agents interact with the system through these key stories:

### Capture Integration
- [US-0203](../stories/capture/US-0203-arxiv-capture.md) — ArXiv Capture (paper metadata extraction)
- [US-0205](../stories/capture/US-0205-wikipedia-capture.md) — Wikipedia Capture (structured article extraction)
- [US-0206](../stories/capture/US-0206-osint-entity-aggregation.md) — OSINT Entity Aggregation (entity resolution)
- [US-0210](../stories/capture/US-0210-cross-platform-entity-resolution.md) — Cross-Platform Entity Resolution (identity matching)

### Discovery & Bootstrap
- [US-0037](../stories/agents/US-0037-agent-discovers-query-schema.md) — Agent Discovers Query Schema
- [US-0038](../stories/agents/US-0038-agent-constructs-rsql-query.md) — Agent Constructs RSQL Query

### Content Ingestion
- [US-0039](../stories/agents/US-0039-agent-ingests-content-and-waits.md) — Agent Ingests Content and Waits

### Enrichment with Constraints
- [US-0009](../stories/enrichment/US-0009-extract-entities-and-mentions.md) — Extract Entities and Mentions
- [US-0010](../stories/enrichment/US-0010-extract-decisions-and-tasks.md) — Extract Decisions and Tasks
- [US-0011](../stories/enrichment/US-0011-assign-tags-from-vocabulary.md) — Assign Tags from Vocabulary
- [US-0012](../stories/enrichment/US-0012-generate-summaries-and-sections.md) — Generate Summaries and Sections
- [US-0013](../stories/enrichment/US-0013-detect-and-extract-code-snippets.md) — Detect and Extract Code Snippets
- [US-0014](../stories/enrichment/US-0014-constrain-extraction-with-lmql.md) — Constrain Extraction with LMQL (token-level hard constraints)
- [US-0046](../stories/enrichment/US-0046-extract-relationships-between-entities.md) — Extract Relationships Between Entities
- [US-0047](../stories/enrichment/US-0047-extract-temporal-information.md) — Extract Temporal Information
- [US-0048](../stories/enrichment/US-0048-detect-sentiment-and-tone.md) — Detect Sentiment and Tone
- [US-0049](../stories/enrichment/US-0049-classify-content-with-taxonomy.md) — Classify Content with Taxonomy
- [US-0050](../stories/enrichment/US-0050-extract-code-metrics-and-complexity.md) — Extract Code Metrics and Complexity

### Search & Retrieval
- [US-0016](../stories/search/US-0016-natural-language-search.md) — Natural Language Search
- [US-0018](../stories/search/US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution
- [US-0019](../stories/search/US-0019-federated-registry-search.md) — Federated Registry Search
- [US-0021](../stories/search/US-0021-search-with-result-explanation.md) — Search with Result Explanation
- [US-0051](../stories/search/US-0051-semantic-search-with-embeddings.md) — Semantic Search with Embeddings
- [US-0052](../stories/search/US-0052-graph-based-entity-search.md) — Graph-Based Entity Search
- [US-0061](../stories/search/US-0061-visual-similarity-search.md) — Visual Similarity Search (multimodal search)

### Composition & Assembly
- [US-0024](../stories/composition/US-0024-compose-with-graph-traversal.md) — Compose with Graph Traversal
- [US-0040](../stories/agents/US-0040-agent-composes-brief-programmatically.md) — Agent Composes Brief Programmatically

### Advanced Integration
- [US-0041](../stories/agents/US-0041-agent-uses-constrained-enrichment.md) — Agent Uses Constrained Enrichment (LMQL integration)

---

## API Contracts

### REST Endpoints
- `GET /query-schema` — Queryable properties, operators, examples for RSQL
- `GET /search?q=...&query_mode=rsql` — Execute RSQL query deterministically
- `GET /search?q=...&query_mode=nlq` — Execute NLQ (auto-detected intent)
- `POST /analyze` — Ingest content and trigger enrichment pipeline
- `GET /objects/{id}` — Retrieve specific object + metadata
- `GET /jobs/{id}` — Poll job status (ingestion, enrichment)

### gRPC Services
- `Search(query_string, query_mode)` → Stream of results
- `Analyze(content_type, raw_content)` → Job ID + async acknowledgment
- `GetJob(job_id)` → Job state, steps, errors
- Similar contracts to REST, optimized for agent-to-service communication

---

## Success Metrics

- **Query determinism:** % of queries with identical result order on repeated execution
- **Enrichment success rate:** % of ingested content with successfully extracted intents/entities/decisions
- **Constraint violation rate:** 0 (hard constraints enforced at generation time)
- **Query latency:** P99 search latency (target: <500ms local, <2s with federated registries)
- **Reranking accuracy:** Agent feedback on result relevance before and after multi-strategy merging

---

## Collaboration with Other Personas

- **Maintainers:** Agents provide feedback on API stability, query schema usefulness, suggest query operator additions
- **Knowledge Workers:** Agents consume knowledge written by humans; humans benefit from agent-curated insights
- **Integrators:** Agents may leverage custom AI providers and ranking algorithms via plugins
- **Operations:** Agents report slow queries and job failures; operations optimize infrastructure
