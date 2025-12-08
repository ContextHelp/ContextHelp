# Sprint 2 Goal: "Real Data, Real Queries"

By the end of this sprint, a user should be able to:

1. Ingest real text and get an LLM-generated summary and tag set.
2. Connect to a remote HTTP registry (not just local files).
3. Perform boolean queries (`tag:ui AND type:text`).
4. Execute **early mention-based queries** (`mention:ui.best-practice`) even though entity resolution and graph integration are not introduced until Sprint 3.

Sprint 2 establishes **schema stability** for mentions, **parser awareness** of mention queries, and **future-proof registry handling** for the entity system introduced later.

---

## 1. Core/Infra Team (The Enabler)

**Focus:** Configuration, Secrets, and Early Semantic Feature Flags.

**Why:** Team 2 needs API keys for LLM providers; Team 4 needs credentials for registries. Core must also expose configuration flags for upcoming semantic and plugin features.

- **Task 1.1: Polymorphic Config Loader**
  - Load configuration from `~/.config/contexthelp/config.yaml`.
  - Implement environment variable overrides (`CH_OPENAI_KEY`, etc.).
  - Add optional feature flags including:
    - `enable_mentions` (default: true — schema and parser should always support mentions).
    - `enable_entity_placeholders` (default: false — entity resolution deferred to Sprint 3).
  - *Deliverable:* A `Config` struct that securely exposes credentials and feature toggles.

- **Task 1.2: Graceful Shutdown**
  - Clean cancellation of long-running LLM or network calls when user presses `Ctrl+C`.

- **Task 1.3: Logging Skeleton**
  - Structured logging via slog/zap.
  - Include hooks for mention-related debug output:
    - parser events
    - AST details
    - placeholder fields injected during ingestion

---

## 2. Ingestion/AI Team (The Brain)

**Focus:** First real pipeline using the decorator pattern and preparing the bookmark schema for semantics.

**Constraint:** Only text ingestion is implemented in this sprint.

**Important:** Mention extraction is *not* implemented yet, but bookmarks must include a stable `mentions: []` array to avoid migrations and to prepare for graph linking in Sprint 3.

- **Task 2.1: AI Client Interfaces (ADR-005)**
  - Define `LLMClient` interface.
  - Implement OpenAI and/or generic HTTP LLM provider.
  - Allow plugin-based providers via config polymorphism (used later, but API must be stable now).

- **Task 2.2: Decorators**
  - Implement `CachingLLMClient`.
  - Implement `RetryLLMClient` with exponential backoff.
  - Ensure all decorators are chainable and compatible with upcoming plugin-provided providers.

- **Task 2.3: `text.short` Pipeline**
  - Replace `text.echo` with `text.short`.
  - Pipeline steps:
    - summarize input text
    - extract tags
    - prepare bookmark fields
  - Bookmark schema must now include:
    - `mentions: []`
    - `entities: []` *(reserved for Sprint 3)*
  - Persist using the storage layer defined in Sprint 1.

---

## 3. Search/Retrieval Team (The Parser)

**Focus:** The RSQL AST & Query System.

**Added requirement:** The parser must fully accept and normalize `mention:` filters, even though mention resolution and entity graph queries are not functional yet.

- **Task 3.1: RSQL Lexer/Parser (ADR-010)**
  - Implement grammar for:
    - fields: `tag:`, `type:`, `lang:`, `mention:`, `pipeline:`
    - operators: `AND`, `OR`, `NOT`, `*`, parentheses
  - The AST must include a field node for `mention:` so Sprint 3 can attach entity resolution rules cleanly.

- **Task 3.2: AST → SQL Transpiler**
  - Convert AST into safe parameterized SQL.
  - Map `mention:` to filtering against the `mentions` JSON column:
    - wildcard support (`mention:ui.*`)
    - exact match (`mention:ui.best-practice`)
  - SQL should run cleanly even when mentions arrays are empty.

- **Task 3.3: Integrate with `ch list --q`**
  - Queries like `mention:ui.best-practice` must:
    - not error
    - produce valid SQL
    - return empty sets until Sprint 3 adds real mentions

---

## 4. Registry/Ecosystem Team (The Network)

**Focus:** Remote registry support, caching, merge logic, and architectural readiness for entities.

**Extended requirement:** Although registries only expose taxonomies during Sprint 2, the underlying architecture must prepare for the entity system introduced in Sprint 3.

- **Task 4.1: Registry HTTP Client**
  - Implement a robust HTTP client for registry endpoints.
  - Handle:
    - headers
    - auth tokens
    - TLS options
  - Client must be structured so adding `/entities` endpoints in Sprint 3 does not require large refactors.

- **Task 4.2: Registry Caching Strategy**
  - Implement TTL or S-W-R caching.
  - Cache format must reserve:
    - space for entities
    - alias maps
    - provenance metadata

- **Task 4.3: Taxonomy Merger**
  - Merge remote registry taxonomies with local rules.
  - Local wins in conflict.
  - Add placeholder functions (`mergeEntities`, `mergeAliases`) for Sprint 3 to activate.

---

## The Integration Check (The Demo)

**Scenario:** The “Smart Note” Test with Early Mention Query Support.

1. **Setup:**
   `config.yaml` contains OpenAI key and registry URL.

2. **Action:**
   ```
   ch analyze --text "Refactoring the login flow to reduce friction." --hints "#ux"
   ```

3. **Behind the Scenes:**
   - Config loads including mention feature flags.
   - `text.short` pipeline runs:
     - summary
     - tag extraction
     - writes bookmark with `mentions: []`.
   - Registry taxonomy loaded/cached.

4. **Action:**
   ```
   ch list --q "tag:ux* AND created:>2025-01-01"
   ```

5. **Action (new):**
   ```
   ch list --q "mention:ui.best-practice"
   ```
   Expected behavior:
   - Parser accepts the query.
   - SQL is generated cleanly.
   - Storage returns 0 results (as no mentions exist yet).

6. **Result:**
   Mention queries work without requiring entity resolution.

---

## Risks to Watch For

- **Parser complexity:** RSQL grammar must accept mention fields even before graph support exists.
- **Schema stability:** Adding `mentions` early avoids migrations later; ingestion must not populate incorrect placeholders.
- **Future-proofing:** Registry caching must be flexible enough to add entity data next sprint without breaking existing caches.
- **Cross-team alignment:** Parser, ingestion, and registry teams must all use the same mention-related schema expectations despite differing levels of implementation.