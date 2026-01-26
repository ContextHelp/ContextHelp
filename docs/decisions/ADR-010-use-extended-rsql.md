# ADR-010 – Adopt RSQL as the Base Query Language with ContextHelp Extensions

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp requires a robust, expressive, and safe query language for searching across:

- local bookmarks
- decentralized registries
- plugin-defined knowledge sources
- hybrid metadata + full-text + semantic/vector search

Previously, we considered designing a proprietary query language. Early prototypes demonstrated clear shortcomings:

- string-based parsing was ambiguous and fragile
- supporting nested Boolean logic required increasingly complex ad-hoc parsing
- registry-based fields (`tag:…`, `pipeline:…`, `registry:…`) needed a standardized way to map to internal structures
- agents (LLMs) require consistent, well-structured query syntax to interoperate predictably
- plugins require a safe extension mechanism to add new operators

Additionally, ContextHelp’s decentralized architecture means queries may route to:

- SQLite metadata filters
- SQLite FTS
- vector search indices
- remote registry endpoints
- custom plugin sources
- reranking layers

A formal AST and clear grammar were needed to support a unified query planning system.
Among the candidates evaluated—Lucene syntax, Meilisearch QL, SPARQL, OData, RSQL, and custom DSL—RSQL offered the strongest foundation:

- well-specified grammar
- industry adoption
- Boolean logic with parentheses
- comparisons (`=`, `!=`, `<`, `<=`, etc.)
- `in`/`out` operators
- formally parseable into ASTs
- extensible for domain-specific operators

Given these needs and constraints, a standardized base grammar with ContextHelp-specific extensions provides the best balance of interoperability, safety, and expressiveness.

Subsystems impacted:

- Search service
- Query parser & AST builder
- Storage layer (SQL/FTS)
- Registry integration layer
- Reranking & result merging
- Plugins that define operators or fields
- CLI, REST, and gRPC APIs

Goals optimized for:

- correctness
- interoperability
- extensibility
- consistency across local and remote sources
- reliability for agents generating queries
- long-term maintainability

---

## Decision

**We will adopt RSQL as the base query language and extend it with ContextHelp-specific operators, fields, and semantic search capabilities.**

---

## Rationale

### Why RSQL?
- It provides a *formal grammar* and is testable, parseable, and predictable.
- Well-suited for metadata querying (dates, types, tags, languages).
- Already maps naturally to SQL and AST-based execution.
- Extensible—operators like `similar==` or `pipeline==` can be added.
- Familiar enough for developers and LLMs to generate reliably.

### Why not a custom DSL?
- Higher maintenance cost.
- Harder for agents to learn and generate correctly.
- Ambiguities and incompatibilities surface quickly when mixing Boolean logic and nested filters.
- Custom semantics would require ongoing documentation, tooling, and education.

### Why not Lucene, SPARQL, or OData?
- Lucene syntax assumes a full-text search backend and does not map well to multi-source retrieval.
- SPARQL is overpowered and infrastructure-heavy for local-first systems.
- OData is tightly coupled to REST APIs and overly verbose for terminal or agent usage.

### Benefits of RSQL + Extensions
- Formal AST → safe query execution plan.
- Works for both humans and agents.
- Ability to support plugin-defined fields and operators.
- Compatible with metadata, vector, and registry-based searches.
- Reduces ad-hoc parsing and string-matching errors.
- Aligns with long-term goals of decentralization and multi-source search.

### Risks / Drawbacks
- Requires a custom extension layer for semantics not covered by RSQL (e.g., semantic similarity).
- Users unfamiliar with RSQL may require minor onboarding.
- The parser must handle ambiguous cases and error reporting cleanly.

---

## Consequences

### Positive
- Unified query model across CLI, REST, gRPC, and plugins.
- Improves safety and correctness by eliminating string-based parsing.
- Enables hybrid search (SQL + FTS + vector + registries).
- Strong foundation for agents generating structured filters.
- Extensible to future features (ranking hints, semantic scopes, registry-namespaced fields).

### Negative
- Additional implementation work to integrate RSQL grammar with ContextHelp’s AST and planning system.
- Requires documentation and examples for users unfamiliar with RSQL.
- Plugins must adhere to a stricter operator and field registration mechanism.

### Neutral / Considerations
- Registry-specific fields must map cleanly into the AST; naming conventions will matter.
- Query performance depends on optimizing pushdown filters into SQL/FTS.
- Multi-source execution requires deterministic operator semantics across backends.

---

## Implementation Notes

- Use an existing Go-compatible RSQL parser or build one using a parser combinator library (e.g., `participle`).
- Convert parsed RSQL → ContextHelp AST with nodes for:
  - Boolean operators
  - comparisons
  - in/out sets
  - extensions (`similar`, `pipeline`, `registry`, `lang`, etc.)
- Map AST → QueryPlan:
  - SQL metadata filters
  - FTS search
  - registry queries
  - optional vector search
- Merge results via reranker (e.g., RRF).
- Provide CLI validation errors with caret/specific token location.
- Update REST and gRPC APIs to accept RSQL syntax via `q=...` parameters.
- Add documentation in `query-language-spec.md` and CLI help.
- Test using fixtures covering precedence, parentheses, errors, and plugin operators.

---

## References

- RSQL Specification: https://github.com/jirutka/rsql-parser
- ADR-011 – Reranking Layer for Multi-Source Result Merging
- ADR-003 – Separate Read & Write Paths
- ADR-009 – Multi-Source Retrieval Architecture
- ContextHelp Query Language Draft
- **ADR-014 – Two-Package Architecture (dPKMS + ctxt)**
- Internal analysis of Microsoft Kernel Memory query model

---