# ADR-050 – RSQL as Canonical Query Grammar for Phase 6

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

The query system documentation describes two query paths:

1. **RSQL (Structured):** Deterministic, agent-friendly syntax using `;` for AND, `,` for OR, `==`, `!=`, `=in=`, `=out=`, `>=`, `<=` operators. Defined in `query-language-spec.md` and `design.md`.

2. **NLQ (Natural Language):** Human-friendly queries processed by an AI-backed normalizer that classifies intent, extracts entities, and selects retrieval strategies. Requires a functional AI provider integration.

Both paths are well-documented, but they serve different audiences and have different implementation dependencies:

- **RSQL** is self-contained — needs only a parser and AST→SQL transpiler.
- **NLQ** requires AI provider config, intent classification model, entity extraction, strategy selection, and LMQL/instructor/outlines integration — all of which depend on the pipeline runtime and AI provider subsystem being functional first.

Phase 6 implements the core business logic. The query engine needs a working parser to test storage, search, and the API layer end-to-end.

**The question:** Which grammar should be implemented first as the canonical Phase 6 parser target?

---

## Decision

**RSQL is the canonical query grammar for Phase 6.** The parser will implement RSQL syntax as specified in `query-language-spec.md`. NLQ will be implemented in a later phase once AI providers and the pipeline runtime are operational.

### RSQL Syntax (Phase 6 Scope)

```
# Equality / inequality
type==article
type!=draft

# Range operators
created_at>=2025-01-01
weight>0.8

# Set operators
tag=in=(design,ux,patterns)
source=out=(twitter,reddit)

# Logical operators
type==article;tag=in=(ui,design)          # AND (;)
pipeline==text.short,pipeline==text.long   # OR (,)

# Grouping
(type==article,type==note);tag==important

# Extended operators (dPKMS-specific)
mention==@ui.best-practice
entity==@stripe.api
similar=="authentication flow"
```

### NLQ (Deferred)

NLQ is explicitly deferred because:
- It depends on AI provider integration (not yet implemented)
- It depends on LMQL/instructor/outlines for constrained intent classification
- It requires the pipeline runtime for the normalizer step
- RSQL provides a fully functional query path without AI dependencies
- Agents (a primary consumer) prefer RSQL for deterministic, reproducible queries

NLQ will be added as a layer on top of the RSQL engine — the NLQ normalizer will emit a query plan that can include RSQL filters as one of its strategies.

---

## Rationale

### Alternatives Considered

#### 1. Natural Boolean Syntax First (Rejected)

Implement `AND`/`OR`/`NOT` keyword-based parser instead of RSQL operators.

**Rejected because:**
- Overlaps with NLQ (ambiguous whether `find articles about design AND ux` is structured or natural)
- Less standard than RSQL (no established specification)
- `cross-package-contracts.md` explicitly states "RSQL → AST → SQL transpilation"
- Boolean keywords can be added later as syntactic sugar if needed

#### 2. Both Grammars Simultaneously (Rejected)

Build a parser that accepts both RSQL and boolean syntax.

**Rejected because:**
- Increases parser complexity significantly
- Ambiguous inputs (is `type AND tag` a boolean query or a field named "type AND tag"?)
- YAGNI — RSQL alone covers all deterministic query needs
- NLQ handles the human-friendly path better than boolean keywords would

#### 3. Implement NLQ First (Rejected)

Start with the NLQ normalizer and defer RSQL.

**Rejected because:**
- NLQ depends on AI providers, pipeline runtime, and constrained generation — none of which exist yet
- Cannot test the query engine end-to-end without a working AI stack
- Agents need RSQL regardless

### Benefits of Chosen Approach

- **Zero AI dependencies** — the query engine works without any model provider
- **Deterministic** — same query always produces same results
- **Agent-friendly** — agents construct RSQL programmatically
- **Well-specified** — RSQL has a clear grammar and operator set
- **Testable** — parser and transpiler can be fully unit-tested
- **Foundation for NLQ** — NLQ normalizer will emit query plans that include RSQL filters

---

## Consequences

### Positive

- Phase 6 query engine is fully functional without AI provider dependency
- End-to-end testing possible with deterministic queries
- Clean separation: RSQL for agents, NLQ for humans (later)
- Parser implementation is straightforward (recursive descent)

### Negative

- Human users must use RSQL syntax in Phase 6 (less friendly)
- `ctxt find "how does X handle Y"` won't work semantically until NLQ ships
- CLI will need `--query-mode rsql` flag (default) until NLQ is available

### Neutral

- NLQ implementation is not blocked — it can proceed independently once AI providers exist
- The `/query-schema` endpoint (returns available fields/operators) becomes the primary discovery mechanism for agents

---

## Implementation Notes

### Parser Architecture

```go
// Lexer produces tokens from RSQL input
type Lexer struct { /* ... */ }
type Token struct {
    Type  TokenType // FIELD, OPERATOR, VALUE, LPAREN, RPAREN, AND, OR
    Value string
}

// Parser produces AST from token stream
type Parser struct { /* ... */ }

// AST nodes (from query-language-spec.md)
type ComparisonNode struct {
    Field    string
    Operator Operator // EQ, NEQ, GT, GTE, LT, LTE, IN, OUT
    Value    interface{}
}
type LogicalAndNode struct { Children []Node }
type LogicalOrNode  struct { Children []Node }
type NotNode        struct { Child Node }
type MentionNode    struct { EntitySlug string }
type SimilarityNode struct { Text string }
```

### AST → SQL Transpiler

Each AST node maps to SQL:
- `ComparisonNode(type, EQ, "article")` → `WHERE type = 'article'`
- `ComparisonNode(tag, IN, [design,ux])` → `WHERE EXISTS (SELECT 1 FROM json_each(tags) WHERE json_each.value->>'label' IN ('design','ux'))`
- `MentionNode(@ui.best-practice)` → `JOIN edges ... WHERE to_id = 'ui.best-practice'`
- `SimilarityNode("auth flow")` → vector search dispatch (if available) or FTS fallback
- `LogicalAndNode` → `(left) AND (right)`

### Operator Precedence

From RSQL spec: `,` (OR) has lower precedence than `;` (AND).

```
a==1,b==2;c==3  →  a==1 OR (b==2 AND c==3)
```

---

## References

- **dpkms/query-language-spec.md** — Full grammar, AST nodes, dual-path architecture
- **design.md:411-553** — Query language section
- **cross-package-contracts.md:214-254** — QueryEngine interface, "RSQL → AST → SQL transpilation"
- **dpkms/ranking-and-reranking.md** — Result merging after query execution
- ADR-010 – Use Extended RSQL

---
