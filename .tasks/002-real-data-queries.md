# Skeleton 2: Real Data & Queries Tasks

**Package Focus:** dPKMS (50%) + ctxt (50%)

**Goal:** Add intelligence (LLM integration) and query power (RSQL parser). First balanced collaboration between packages.

---

## dPKMS Infrastructure Team (The Enabler)

### Configuration System

- [PLAN] 📦 *dPKMS + ctxt* Polymorphic config loader
  - [ ] Load from `~/.config/contexthelp/config.yaml`
  - [ ] Implement environment variable overrides (`CH_OPENAI_KEY`, etc.)
  - [ ] Add feature flags:
    - [ ] `enable_mentions` (default: true) - schema and parser always support
    - [ ] `enable_entity_placeholders` (default: false) - deferred to Skeleton 3
  - [ ] Secure credential handling (never log API keys)
  - [ ] Config validation on startup
  - [ ] Support for dPKMS and ctxt sections

### Graceful Shutdown

- [ ] 📦 *dPKMS* Implement cancellation for long-running operations
  - [ ] Context propagation through job execution
  - [ ] Clean cancellation of LLM calls on Ctrl+C
  - [ ] Worker shutdown without killing in-progress jobs
  - [ ] Database connection cleanup

### Logging Infrastructure

- [PLAN] 📦 *dPKMS* Structured logging via slog/zap
  - [ ] Configure log levels (debug, info, warn, error)
  - [ ] Add hooks for mention-related debug output:
    - [ ] Parser events
    - [ ] AST details
    - [ ] Placeholder fields during ingestion
  - [ ] Redact secrets from all log output
  - [ ] File and stdout logging targets

---

## ctxt Ingestion Team (The Brain)

### AI Client Interfaces (ADR-005)

- [PLAN] 📦 *ctxt* Define `LLMClient` interface in `ctxt/pkg/ai/`
  - [ ] Define interface:
    ```go
    type LLMClient interface {
        Complete(ctx context.Context, prompt string, opts Options) (string, error)
        Stream(ctx context.Context, prompt string, opts Options) (<-chan string, error)
    }
    ```
  - [ ] Implement OpenAI adapter
  - [ ] Implement generic HTTP LLM provider adapter
  - [ ] Support plugin-based providers via config polymorphism
  - [ ] Add rate limiting and backoff

### Decorator Pattern

- [PLAN] 📦 *ctxt* Implement chainable decorators
  - [ ] `CachingLLMClient`:
    - [ ] Key by prompt hash
    - [ ] TTL-based expiry
    - [ ] In-memory or file-based cache
  - [ ] `RetryLLMClient`:
    - [ ] Exponential backoff
    - [ ] Max attempts configuration
    - [ ] Retry only on transient errors
  - [ ] Ensure decorators compose cleanly
  - [ ] Compatible with future plugin-provided providers

### text.short Pipeline

- [PLAN] 📦 *ctxt* Replace `text.echo` with real pipeline
  - [ ] Pipeline steps:
    1. [ ] Summarize input text via LLM
    2. [ ] Extract tags via LLM (structured output)
    3. [ ] Prepare KnowledgeObject fields
    4. [ ] **Include `mentions: []` (empty for now)**
    5. [ ] **Include `entities: []` (reserved for Skeleton 3)**
  - [ ] Call tag enrichment via dPKMS registry
  - [ ] Persist via dPKMS storage layer
  - [ ] Log pipeline execution to job_steps

---

## dPKMS Search Team & ctxt Retrieval Team (The Parser)

### RSQL Lexer/Parser (ADR-010)

- [PLAN] 📦 *dPKMS* Implement query parser in `dpkms/pkg/query/`
  - [ ] Define grammar supporting:
    - [ ] Field filters: `tag:`, `type:`, `lang:`, `mention:`, `pipeline:`
    - [ ] Boolean operators: `AND`, `OR`, `NOT`
    - [ ] Wildcards: `*` in values
    - [ ] Parentheses for grouping
  - [ ] Build AST from query string
  - [ ] **AST must include `mention:` field node (even though resolution not functional yet)**
  - [ ] Validate query syntax
  - [ ] Return structured parse errors

### AST → SQL Transpiler

- [PLAN] 📦 *dPKMS* Implement SQL generation
  - [ ] Convert AST to parameterized SQL
  - [ ] Map field filters to column names
  - [ ] **Map `mention:` to JSON filtering on `mentions` column:**
    - [ ] Support wildcard patterns: `mention:ui.*`
    - [ ] Support exact match: `mention:ui.best-practice`
    - [ ] SQL must run cleanly even when mentions arrays are empty
  - [ ] Prevent SQL injection via parameterization
  - [ ] Optimize generated queries

### CLI Query Integration

- [ ] 📦 *ctxt → dPKMS* Implement `ctxt list --q` flag
  - [ ] Accept RSQL query string from user
  - [ ] Pass to dPKMS query engine
  - [ ] Display results in table format
  - [ ] **Queries like `mention:ui.best-practice` must:**
    - [ ] Not error (dPKMS parser accepts it)
    - [ ] Produce valid SQL (dPKMS transpiler)
    - [ ] Return empty sets until Skeleton 3 adds real mentions

---

## dPKMS Registry Team (The Network)

### Registry HTTP Client

- [PLAN] 📦 *dPKMS* Implement HTTP client in `dpkms/pkg/registry/http.go`
  - [ ] Robust HTTP client for registry endpoints
  - [ ] Support:
    - [ ] Custom headers
    - [ ] Auth tokens (Bearer, API key)
    - [ ] TLS options
    - [ ] Timeout configuration
  - [ ] **Structure client to easily add `/entities` endpoints in Skeleton 3**
  - [ ] Error handling for network failures

### Registry Caching Strategy

- [PLAN] 📦 *dPKMS* Implement caching layer
  - [ ] TTL or Stale-While-Revalidate caching
  - [ ] Cache storage (file-based or in-memory)
  - [ ] **Cache format must reserve space for:**
    - [ ] Entity definitions
    - [ ] Alias maps
    - [ ] Provenance metadata
  - [ ] Cache invalidation logic
  - [ ] Offline fallback to cache

### Taxonomy Merger

- [PLAN] 📦 *dPKMS* Implement merge logic
  - [ ] Merge remote taxonomy with local rules
  - [ ] Conflict resolution: local wins
  - [ ] **Add placeholder functions for Skeleton 3:**
    - [ ] `mergeEntities()` (stub)
    - [ ] `mergeAliases()` (stub)
  - [ ] Preserve provenance metadata
  - [ ] Deterministic merge order

---

## Integration Check (Smart Note Test)

### Setup

1. [ ] Configure `~/.config/contexthelp/config.yaml` with:
   - [ ] OpenAI API key
   - [ ] Registry URL
   - [ ] Mention feature flags enabled

### Scenario 1: Real Pipeline Execution

1. [ ] Run:
   ```bash
   ctxt analyze --text "Refactoring the login flow to reduce friction." --hints "#ux"
   ```

2. [ ] Verify:
   - [ ] Config loads including mention flags
   - [ ] `text.short` pipeline executes:
     - [ ] LLM generates summary
     - [ ] Tags extracted
     - [ ] KnowledgeObject written with `mentions: []`
   - [ ] Registry taxonomy loaded/cached
   - [ ] Job completes successfully

### Scenario 2: Query with Mentions

1. [ ] Run:
   ```bash
   ctxt list --q "tag:ux* AND created:>2025-01-01"
   ```
   - [ ] Verify query parses and executes
   - [ ] Results displayed

2. [ ] Run:
   ```bash
   ctxt list --q "mention:ui.best-practice"
   ```
   - [ ] Verify parser accepts query
   - [ ] SQL generated cleanly
   - [ ] Returns 0 results (no mentions exist yet)
   - [ ] No errors

### Validation Checklist

**dPKMS Validation:**
- [ ] RSQL parser accepts `mention:` operator
- [ ] AST → SQL transpiler generates valid mention queries
- [ ] Registry HTTP client fetches remote taxonomies
- [ ] Registry caching operational
- [ ] Config loader reads both dPKMS and ctxt sections
- [ ] Graceful shutdown works for long operations

**ctxt Validation:**
- [ ] AI client interfaces testable
- [ ] LLM decorators (caching, retry) functional
- [ ] `text.short` pipeline produces summaries and tags
- [ ] Pipeline output includes `mentions: []` field
- [ ] Pipeline persists via dPKMS storage

**Cross-Package Validation:**
- [ ] ctxt queries dPKMS using RSQL
- [ ] ctxt pipelines write to dPKMS storage
- [ ] dPKMS returns query results to ctxt
- [ ] Shared configuration works

---

## Risks to Watch For

- **Parser Complexity:** RSQL must accept mention fields before graph support exists
- **Schema Stability:** Adding `mentions` early avoids migrations; don't populate incorrect data
- **Future-Proofing:** Registry caching must handle entity data next skeleton without breaking
- **Cross-Team Alignment:** Parser, ingestion, registry teams must align on mention schema expectations

---

## See Also

- Sprint spec: `docs/sprints/002-real-data-and-queries.md`
- ADR-005: AI client abstraction
- ADR-010: RSQL query language
- Cross-package contracts: `docs/sprints/CROSS-PACKAGE-CONTRACTS.md`
