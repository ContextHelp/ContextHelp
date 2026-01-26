# ADR-011 – Introduce a Dedicated Reranker Layer for Multi-Source Result Merging

> **Status:** Accepted
> **Date:** 2025-10-05
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

ContextHelp retrieves knowledge from multiple heterogeneous sources:

- the **local bookmark store** (SQLite or any configured backend)
- **registries** (remote or local) that may provide taxonomy-aware results
- **plugin-defined sources** (e.g., LLM-powered virtual indexes, custom stores)
- **future vector-search engines** (local or remote)

Each source:
- uses a **different scoring model**
- has **different recall properties**
- may return **overlapping or duplicate items**
- may return **partial context** or fields unavailable elsewhere
- may sort results differently (recency, semantic similarity, popularity)
- may apply **registry-specific heuristics** or weights

Without a unified merging system:
- users see inconsistent ordering
- agents receive non-deterministic result ranking
- duplicates appear (same URL, same tag, same canonical ID across registries)
- complex queries may give conflicting interpretations across sources
- performance varies unpredictably based on backend ordering

The architecture also needs to support:
- **scatter–gather search** across sources
- **query-language AST** generating multiple query plans
- **multi-hop retrieval** (registry → related patterns → local matches)
- **local-first ranking rules** (e.g., user’s own bookmarks prioritized)
- **agent-friendly scoring** (stable scores with clear semantics)

Additionally, different retrieval types (metadata, FTS, embeddings, examples, decisions, tags) must be merged meaningfully.

Because ContextHelp is **decentralized**, **plugin-extensible**, and **registry-driven**, no single search engine or SQL query can handle the complete ranking logic.

A dedicated reranking layer is required to merge disparate result sets into a unified, deterministic, high-quality response.

---

## Decision

**We will introduce a dedicated Reranker Layer responsible for merging, deduplicating, and reordering results from multiple sources (local storage, registries, vector stores, plugin-defined sources).**

This reranker becomes the canonical stage after all retrieval streams complete.

---

## Rationale

### Why a Reranker Layer?

1. **Different sources produce incompatible scoring models**
   - SQL returns no scores or simplistic recency ordering
   - registries may return domain-specific confidence weights
   - vector stores return similarity scores
   - plugins may return heuristic relevance values

2. **Duplicates appear across sources**
   - The same URL appears in local bookmarks and remote registries
   - The same taxonomy tag appears in multiple registries
   - Canonical IDs may diverge

   Deduplication must be centralized.

3. **Users expect consistent ordering**
   A unified ranking strategy improves reliability for both humans and agents.

4. **Supports hybrid search pipelines**
   Combining:
   - metadata filters
   - full-text search
   - semantic (embedding) search
   - registry-based domain retrieval

5. **Supports decentralized registries without forcing sync**
   Multi-source retrieval is only viable if there is a powerful merge step.

6. **Allows experimentation**
   Reranking can evolve independently without breaking storage or pipeline code.

### Alternatives Rejected

#### A. *“Just rely on SQL sorting”*
- Cannot merge remote results
- Cannot integrate embedding scores
- No deduplication
- Not viable for multi-source or decentralized search

#### B. *“Let each registry decide ordering”*
- Produces inconsistent UX
- Cannot unify results across registries
- No interoperability with local bookmarks

#### C. *“Use a single global index” (sync everything locally)*
- Heavy storage requirements
- Loses freshness
- Breaks business model for paid/private registries
- Hard to maintain consistency and conflict resolution

### Why centralized reranking is the right tradeoff?

It allows:
- consistent UX
- decoupled evolution
- plugin extensibility
- hybrid search
- decentralized architecture

---

## Consequences

### Positive

- **Unified search experience** — ordering is consistent regardless of source.
- **Supports decentralization** — registries do not need to expose uniform scoring.
- **Better agent reasoning** — predictable ranking improves automated decisions.
- **Extensibility** — new retrieval sources can be integrated without modifying core search logic.
- **Improved deduplication** — canonical rules applied once in a single place.
- **Supports hybrid metadata + semantic search**.

### Negative

- **Adds architectural complexity** — additional component with its own logic.
- **Requires careful performance tuning** — reranking may become a bottleneck for large result sets.
- **Debugging complexity** — reranking must be transparent and explainable.
- **Introduces dependencies on scoring heuristics** — subjective weighting must be configurable.

### Neutral / Considerations

- Reranking must support **partial responses** when registries time out.
- Requires clear APIs for plugins to provide scores and metadata.
- Might need caching once results are stable.

---

## Implementation Notes

- The reranker will accept a list of `SearchResult` objects containing:
  - canonical ID
  - source
  - raw score(s)
  - recency data
  - view/match metrics
  - registry weighting
  - semantic similarity (optional)
  - deduplication keys (URL, canonical tag, alias sets)

- Candidate algorithms:
  - **Reciprocal Rank Fusion (RRF)**
  - **Weighted score blending**
  - **Boosts for:**
    - user's own bookmarks
    - recent views
    - strong tag matches
    - preferred languages

- Deduplication must use:
  - URL normalization
  - registry-provided canonical IDs
  - vector-similarity dedupe threshold (optional)

- This layer will be placed between:
  - Query execution → Reranker → Result pagination

- Logging/explainability should support:
  - “Why did this result rank #1?”
  - “Which scores contributed?”

- Testing:
  - Use deterministic seeds
  - Compare ranking stability across queries
  - Validate deduplication at multiple thresholds

---

## References

- ADR-009 – Multi-Source Retrieval as Default
- ADR-010 – Query Language (RSQL Extensions)
- Kernel Memory reranking strategies
- Reciprocal Rank Fusion research:
  - *Cormack et al., "Reciprocal Rank Fusion Outperforms Condorcet and Individual Rank Learning Methods"*
- ElasticSearch multi-index search merging heuristics
- Registry Protocol Specification (WIP)

---