# Skeleton 2 Goal: "Real Data, Real Queries"

## Package Focus

**Balanced:** dPKMS (50%) + ctxt (50%)

This skeleton represents the first true collaboration between packages. ctxt adds intelligence (LLM integration, pipeline recipes), while dPKMS adds query power (RSQL parser, AST → SQL compilation).

**Package Breakdown:**
- **dPKMS:** Configuration loader, RSQL parser, query engine, registry HTTP client, caching
- **ctxt:** AI client interfaces, pipeline decorators, text.short pipeline, tag enrichment logic

---

By the end of this skeleton, a user should be able to:

1. Ingest real text and get an LLM-generated summary and tag set.
2. Connect to a remote HTTP registry (not just local files).
3. Perform boolean queries (`tag:ui AND type:text`).
4. Execute **early mention-based queries** (`mention:ui.best-practice`) even though entity resolution and graph integration are not introduced until Skeleton 3.

Skeleton 2 establishes **schema stability** for mentions, **parser awareness** of mention queries, and **future-proof registry handling** for the entity system introduced later.

---

## 1. dPKMS Infrastructure Team (The Enabler)

**Focus:** Configuration, Secrets, and Early Semantic Feature Flags.

**Why:** Team 2 needs API keys for LLM providers; Team 4 needs credentials for registries. Core must also expose configuration flags for upcoming semantic and plugin features.

- **Task 1.1: Polymorphic Config Loader** 📦 *dPKMS + ctxt (shared config)*
  - Load configuration from `~/.config/contexthelp/config.yaml` (dPKMS config loader).
  - Implement environment variable overrides (`CH_OPENAI_KEY`, etc.) (dPKMS).
  - Add optional feature flags including:
    - `enable_mentions` (default: true — schema and parser should always support mentions) (dPKMS).
    - `enable_entity_placeholders` (default: false — entity resolution deferred to Skeleton 3) (dPKMS).
  - *Deliverable:* A `Config` struct that securely exposes credentials and feature toggles.
  - *Package:* dPKMS provides config loading; ctxt consumes config for AI providers.

- **Task 1.2: Graceful Shutdown** 📦 *dPKMS*
  - Clean cancellation of long-running LLM or network calls when user presses `Ctrl+C` (dPKMS job cancellation).

- **Task 1.3: Logging Skeleton** 📦 *dPKMS*
  - Structured logging via slog/zap (dPKMS logging infrastructure).
  - Include hooks for mention-related debug output:
    - parser events (dPKMS)
    - AST details (dPKMS)
    - placeholder fields injected during ingestion (ctxt → dPKMS)

---

## 2. ctxt Ingestion Team (The Brain)

**Focus:** First real pipeline using the decorator pattern and preparing the knowledge_object schema for semantics.

**Constraint:** Only text ingestion is implemented in this skeleton.

**Important:** Mention extraction is *not* implemented yet, but knowledge objects must include a stable `mentions: []` array to avoid migrations and to prepare for graph linking in Skeleton 3.

- **Task 2.1: AI Client Interfaces (ADR-005)** 📦 *ctxt*
  - Define `LLMClient` interface (ctxt AI abstraction layer).
  - Implement OpenAI and/or generic HTTP LLM provider (ctxt).
  - Allow plugin-based providers via config polymorphism (ctxt extensibility).

- **Task 2.2: Decorators** 📦 *ctxt*
  - Implement `CachingLLMClient` (ctxt decorator).
  - Implement `RetryLLMClient` with exponential backoff (ctxt decorator).
  - Ensure all decorators are chainable and compatible with upcoming plugin-provided providers.

- **Task 2.3: `text.short` Pipeline** 📦 *ctxt pipeline → dPKMS storage*
  - Replace `text.echo` with `text.short` (ctxt pipeline definition).
  - Pipeline steps (ctxt):
    - summarize input text
    - extract tags
    - prepare knowledge object fields
  - Knowledge object schema must now include (dPKMS schema):
    - `mentions: []`
    - `entities: []` *(reserved for Skeleton 3)*
  - Persist using the dPKMS storage layer defined in Skeleton 1.

---

## 3. dPKMS Search Team & ctxt Retrieval Team (The Parser)

**Focus:** The RSQL AST & Query System.

**Added requirement:** The parser must fully accept and normalize `mention:` filters, even though mention resolution and entity graph queries are not functional yet.

- **Task 3.1: RSQL Lexer/Parser (ADR-010)** 📦 *dPKMS*
  - Implement grammar for (dPKMS query engine):
    - fields: `tag:`, `type:`, `lang:`, `mention:`, `pipeline:`
    - operators: `AND`, `OR`, `NOT`, `*`, parentheses
  - The AST must include a field node for `mention:` so Skeleton 3 can attach entity resolution rules cleanly.

- **Task 3.2: AST → SQL Transpiler** 📦 *dPKMS*
  - Convert AST into safe parameterized SQL (dPKMS query compiler).
  - Map `mention:` to filtering against the `mentions` JSON column:
    - wildcard support (`mention:ui.*`)
    - exact match (`mention:ui.best-practice`)
  - SQL should run cleanly even when mentions arrays are empty.

- **Task 3.3: Integrate with `ctxt list --q`** 📦 *ctxt → dPKMS*
  - ctxt CLI calls dPKMS query engine with user query string.
  - Queries like `mention:ui.best-practice` must:
    - not error (dPKMS parser accepts it)
    - produce valid SQL (dPKMS transpiler)
    - return empty sets until Skeleton 3 adds real mentions (dPKMS storage)

---

## 4. dPKMS Registry Team (The Network)

**Focus:** Remote registry support, caching, merge logic, and architectural readiness for entities.

**Extended requirement:** Although registries only expose taxonomies during Skeleton 2, the underlying architecture must prepare for the entity system introduced in Skeleton 3.

- **Task 4.1: Registry HTTP Client** 📦 *dPKMS*
  - Implement a robust HTTP client for registry endpoints (dPKMS federation).
  - Handle:
    - headers
    - auth tokens
    - TLS options
  - Client must be structured so adding `/entities` endpoints in Skeleton 3 does not require large refactors.

- **Task 4.2: Registry Caching Strategy** 📦 *dPKMS*
  - Implement TTL or S-W-R caching (dPKMS registry cache).
  - Cache format must reserve:
    - space for entities
    - alias maps
    - provenance metadata

- **Task 4.3: Taxonomy Merger** 📦 *dPKMS*
  - Merge remote registry taxonomies with local rules (dPKMS registry merge logic).
  - Local wins in conflict.
  - Add placeholder functions (`mergeEntities`, `mergeAliases`) for Skeleton 3 to activate.

---

## The Integration Check (The Demo)

**Scenario:** The “Smart Note” Test with Early Mention Query Support.

1. **Setup:**
   `~/.config/contexthelp/config.yaml` contains OpenAI key and registry URL.

2. **Action:**
   ```
   ch analyze --text "Refactoring the login flow to reduce friction." --hints "#ux"
   ```

3. **Behind the Scenes:**
   - Config loads including mention feature flags.
   - `text.short` pipeline runs:
     - summary
     - tag extraction
     - writes knowledge object with `mentions: []`.
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

**Package-Specific Validation:**

**dPKMS Validation:**
- ✅ RSQL parser accepts mention: operator
- ✅ AST → SQL transpiler generates valid mention queries
- ✅ Registry HTTP client fetches remote taxonomies
- ✅ Registry caching strategy operational
- ✅ Configuration loader reads both dPKMS and ctxt sections
- ✅ Graceful shutdown works for long-running operations

**ctxt Validation:**
- ✅ AI client interfaces defined and testable
- ✅ LLM decorators (caching, retry) work correctly
- ✅ text.short pipeline produces summaries and tags
- ✅ Pipeline output includes mentions: [] field
- ✅ Pipeline persists via dPKMS storage layer

**Cross-Package Validation:**
- ✅ ctxt queries dPKMS using RSQL query strings
- ✅ ctxt pipelines write to dPKMS storage
- ✅ dPKMS returns query results to ctxt for display
- ✅ Shared configuration loaded by both binaries

---

## Risks to Watch For

- **Parser complexity:** RSQL grammar must accept mention fields even before graph support exists.
- **Schema stability:** Adding `mentions` early avoids migrations later; ingestion must not populate incorrect placeholders.
- **Future-proofing:** Registry caching must be flexible enough to add entity data next skeleton without breaking existing caches.
- **Cross-team alignment:** Parser, ingestion, and registry teams must all use the same mention-related schema expectations despite differing levels of implementation.
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
