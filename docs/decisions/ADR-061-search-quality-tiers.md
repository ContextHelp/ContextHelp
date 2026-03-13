# ADR-061 – Search Quality Tiers as a First-Class Query Concept

> **Status:** Accepted
> **Date:** 2026-03-13
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

The system supports two query paths today (ADR-010, ADR-050):

1. **RSQL** — structured, deterministic, agent-friendly
2. **NLQ** — natural language, AI-backed normalizer, human-friendly

Both paths ultimately produce a query plan that dispatches to retrieval strategies (lexical BM25, vector similarity, graph traversal) and a reranker (ADR-011). However, the current design does not formally distinguish between fast/cheap retrieval and slow/high-quality retrieval. All queries implicitly go through the same pipeline regardless of the caller's latency tolerance or quality needs.

This design gap was surfaced while evaluating [qmd](https://github.com/tobi/qmd), which explicitly exposes three search modes — `search` (lexical only), `vsearch` (vector only), `query` (hybrid + LLM reranking) — as first-class CLI commands with clearly communicated speed/quality trade-offs. Users and agents choose based on their need.

In our system, agents may need sub-100ms lookups for inline context assembly, while a human composing a brief can tolerate 2–5 seconds for maximum quality. These are fundamentally different operating modes that should be explicitly addressable.

---

## Decision

**Search quality tiers are a first-class concept in the query API.** The query engine exposes three tiers, selectable via the `quality` parameter:

| Tier | Name | Strategies | Reranking | Approx. Latency |
|------|------|-----------|-----------|-----------------|
| 1 | `fast` | Lexical (BM25) only | None | < 50ms |
| 2 | `balanced` | Lexical + Vector (RRF fusion) | None | 100–500ms |
| 3 | `best` | Lexical + Vector + Graph (RRF fusion) | LLM reranker | 1–5s |

Default tier is `balanced` unless overridden.

### Query API

```
GET /search?q=<query>&quality=fast|balanced|best
```

CLI:

```bash
ctxt find "authentication flow"              # balanced (default)
ctxt find "authentication flow" --fast       # lexical only
ctxt find "authentication flow" --best       # full pipeline + reranking
```

Programmatic (Go):

```go
results, err := engine.Search(ctx, SearchRequest{
    Query:   "authentication flow",
    Quality: QualityBalanced, // QualityFast | QualityBalanced | QualityBest
})
```

### Tier Definitions

**`fast`** — Lexical BM25 only. No embeddings, no graph traversal, no LLM calls. Suitable for agents doing inline context assembly, autocomplete, or existence checks. Returns in < 50ms even on large corpora.

**`balanced`** — BM25 + vector similarity, fused via Reciprocal Rank Fusion (RRF). No LLM calls. Covers most human and agent queries well. This is the default.

**`best`** — Full pipeline: BM25 + vector + graph traversal, RRF fusion, then LLM reranking. Used for composition briefs, research summaries, or any task where result quality justifies latency. Requires a configured AI provider.

### NLQ Auto-Tier

When `query_mode=nlq`, the NLQ normalizer may override the caller-specified tier:
- If the classified intent is `lookup` or `existence` → downgrades to `fast` automatically
- If the classified intent is `compose` or `research` → upgrades to `best` automatically
- Otherwise, respects the caller's specified tier

This keeps the API simple while allowing the NLQ layer to apply domain knowledge about intent-appropriate quality.

---

## Rationale

### Why make tiers explicit rather than implicit

The alternative — always running the full pipeline and optimizing internally — forces every caller to pay the worst-case latency even when they don't need it. Agents assembling inline context at invocation time cannot afford 2–5 seconds per call. Explicit tiers give callers control and set clear latency contracts.

### Why three tiers (not two, not four)

- **Two** (fast/slow) loses the important middle ground: BM25+vector without reranking covers the majority of queries well and avoids the LLM dependency.
- **Four or more** creates decision paralysis for callers. Three maps cleanly to: "I need speed", "I need good results", "I need the best results possible".

### Why `balanced` as default

Most queries — from both humans and agents — benefit from semantic matching (vector) in addition to keyword matching, but don't require LLM reranking. `balanced` provides the best latency/quality trade-off for the common case.

### Alignment with existing architecture

- ADR-011 (multi-source reranker) defines the reranker used in `best` tier
- ADR-022 (vector semantic search) defines the vector retrieval used in `balanced` and `best`
- ADR-010/ADR-050 (RSQL/NLQ) define query parsing — tiers operate orthogonally to query grammar
- The NLQ normalizer (design.md) already classifies intent — tier auto-selection is a natural extension

---

## Consequences

### Positive

- Agents get a predictable, low-latency path (`fast`) without needing to know search internals
- Human composition workflows get a high-quality path (`best`) with a clear opt-in
- Latency contracts are explicit — callers can set expectations and timeout budgets
- NLQ auto-tier allows smart defaults without burdening callers with intent classification

### Negative

- Three code paths to test per query grammar (RSQL × 3 tiers, NLQ × 3 tiers)
- `best` tier requires an AI provider — callers on `fast`/`balanced` have no AI dependency, but `best` fails gracefully if no provider is configured (falls back to `balanced` with a warning)
- Documentation burden: users must understand the trade-offs to choose correctly

### Neutral

- `fast` and `balanced` are fully functional without any AI provider — aligns with local-first (ADR-001)
- The tier selection is a query-time parameter, not a configuration — no restart required to switch
- Tier metadata is included in the search response (`tier_used`, `latency_ms`) for observability

---

## Implementation Notes

### Query Engine

```go
type QualityTier int

const (
    QualityFast     QualityTier = 1 // BM25 only
    QualityBalanced QualityTier = 2 // BM25 + vector, RRF (default)
    QualityBest     QualityTier = 3 // BM25 + vector + graph, RRF + LLM rerank
)

type SearchRequest struct {
    Query      string
    QueryMode  QueryMode   // RSQL | NLQ | Auto
    Quality    QualityTier
    Limit      int
    MinScore   float64
    // ...
}

type SearchResponse struct {
    Results   []SearchResult
    TierUsed  QualityTier `json:"tier_used"`
    LatencyMS int64       `json:"latency_ms"`
}
```

### Tier Dispatch

```go
func (e *QueryEngine) Search(ctx context.Context, req SearchRequest) (SearchResponse, error) {
    switch req.Quality {
    case QualityFast:
        return e.searchLexical(ctx, req)
    case QualityBalanced:
        return e.searchHybrid(ctx, req) // BM25 + vector, no rerank
    case QualityBest:
        results, err := e.searchHybrid(ctx, req)
        if err != nil { return results, err }
        return e.rerank(ctx, results, req)
    }
}
```

### `best` Tier Fallback

If no AI provider is configured and `quality=best` is requested:
- Falls back to `balanced`
- Sets `tier_used=balanced` in response
- Adds `"warnings": ["reranking unavailable: no AI provider configured"]`

---

## References

- ADR-010 – Use Extended RSQL
- ADR-011 – Use Multi-Source Reranker
- ADR-022 – Vector Semantic Search
- ADR-050 – RSQL as Canonical Query Grammar for Phase 6
- design.md – Query Plan Compilation & Execution, Query Parameters (Search)
- [qmd](https://github.com/tobi/qmd) — source of the progressive quality tier pattern

---
