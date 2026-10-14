# Ranking and Reranking

This document describes how ContextHelp performs multi-source retrieval using a structured ranking and reranking pipeline. Retrieval in ContextHelp is not limited to local bookmark storage: results may come from local stores, registries, external indexes, and vector or embedding-backed stores. Because each source produces results with different scoring semantics, ContextHelp normalizes, merges, deduplicates, and reranks results to produce a unified, predictable response.

---

## Overview

ContextHelp retrieval follows a **scatter–gather** model:

1. **Scatter:**
   Multiple sources are queried independently:
   - Local bookmark store (SQL/FTS)
   - Registry search endpoints (taxonomy, bookmarks, weights)
   - Vector/semantic stores
   - Cached views

2. **Gather:**
   Results are collected into a unified candidate set.

3. **Score Normalization:**
   Each source’s scoring rules differ (FTS rank, semantic similarity, heuristic weights).
   Scores are normalized into a shared range.

4. **Deduplication:**
   Candidates representing the same logical item (canonical URL, same bookmark ID) are merged.

5. **Reranking:**
   A reranker algorithm (RRF, weighted sum, or hybrid scorers) produces the final global ranking.

---

## Retrieval Inputs

Retrieval accepts:

- Query Language AST (structured filters and logical operators)
- Sort strategies (`view`, `match`, `recent`)
- Optional semantic query terms
- Agent-specific settings
- User preferences (language, registry weights, etc.)

These parameters shape the search plan executed by the retrieval layer.

---

## Retrieval Sources

ContextHelp can operate with any number of “retrieval providers.” Examples include:

- **Local Metadata Store** (SQL)
  Filters: type, pipeline, created/edited dates, tags, hints.

- **Local Full-Text Search (FTS)**
  Uses SQLite FTS5 or another backend.

- **Vector Store**
  Embedding-based semantic search (local or remote).

- **Registries (Remote Sources)**
  Registry bookmark endpoints may expose:
  - tag-based search
  - semantic search
  - field filters

Each provider returns:
- A set of candidate bookmarks (or references)
- A provider-native score

---

## Score Normalization

Since different providers use multiplicative, additive, or probabilistic scoring models, ContextHelp normalizes scores into a common 0–1 range.

Normalization options include:

- **Min–Max Scaling**
- **Z-score Transformation**
- **Softmax Normalization**

The retrieval layer allows each provider to define its preferred normalization strategy. Providers may also supply confidence estimates to improve score alignment across sources.

---

## Deduplication

Before reranking, candidates representing the same logical item are merged.

Deduplication strategies include:

- **Bookmark ID Matching**
  (Local + remote references to the same bookmark)

- **Canonical URL Matching**
  Using ContextHelp’s URL normalization rules.

- **Semantic Identity Thresholds**
  Using embeddings to detect near-duplicates across registries.

Merged candidates retain all source scores for reranking.

---

## Reranking Algorithms

ContextHelp supports multiple reranking strategies. Providers can select or override the reranker used by an agent or query context.

### 1. Reciprocal Rank Fusion (RRF)

A robust, source-agnostic method:

```
rrf_score = Σ (1 / (k + rank_i))
```

- Works well when combining high-variance scoring systems.
- Order-based, not magnitude-based.

### 2. Weighted Sum Reranker

Weighted linear combination of normalized scores:

```
score = w1 * semantic + w2 * metadata + w3 * recency + w4 * view_count + w5 * registry_weight
```

Weights may come from:
- Global defaults
- Agent profile configuration
- Registry metadata
- User preferences

### 3. Diminishing Return Reranker

Useful when mixing temporal scoring (recent items) with cumulative scoring (view/match):

```
final_score = base_score * (1 - decay_factor^age) + preference_score
```

---

## Pagination Considerations

Pagination occurs **after reranking**, not at the provider level.

Rationale:
- Provider pagination would bias results in favor of large or aggressive providers.
- Global reranking ensures fairness across all sources.

The pipeline:

```
scatter → gather → normalize → dedupe → rerank → paginate
```

---

## Ranking Signals

Final ranking uses multiple signals:

### Content-Based
- semantic similarity score
- FTS relevance
- tag-based matching
- pipeline subtype relevance

### Behavioral
- `view` count
- `match` count
- recency of views/matches

### Metadata
- created/updated timestamps
- bookmark type (text, url, image, etc.)
- registry-provided weights (e.g., credibility score)

### User Preferences
- preferred languages
- preferred registries
- agent-specific weighting

---

## Implementation Layers

### SearchController
- Accepts query + filters
- Dispatches to SearchPlanner

### SearchPlanner
- Converts AST + options into a multi-provider search plan
- Determines whether to run semantic search, FTS search, or both

### ProviderRouter
- Executes scatter requests concurrently (fan-out)

### MergeEngine
- Collects candidates
- Normalizes scores
- Deduplicates items

### Reranker
- Applies chosen reranking strategy
- Produces globally ranked list

### ResultWindow
- Performs pagination and formatting of final output

---

## Extensibility

Search providers and rerankers are compiled into dpkms
(`internal/ranking.DefaultReranker` adds mention and backlink boosts to the
per-leg RRF scores). There is no registration API and no plugin system
([ADR-078](../decisions/ADR-078-no-plugin-system.md)): a new provider or
reranker is a change to dpkms.

---

## Failure Modes & Recovery

- If a provider fails:
  - Log and continue.
  - Results from healthy providers still proceed.
- If reranker fails:
  - Fallback to simple ordering (recency or view-based).
- If normalization fails:
  - Assign conservative default scoring.

Fault tolerance ensures that one misbehaving provider does not break the entire retrieval process.

---

## Summary

Ranking and reranking are core to ContextHelp’s decentralized retrieval model. By combining scatter–gather search, score normalization, deduplication, and reranking, ContextHelp can:

- Integrate heterogeneous search sources
- Produce high-quality, unified rankings
- Respect agent preferences and domain-specific rules
- Remain extensible through registries

This design enables ContextHelp to serve as a robust, future-proof context engine in a multi-registry, multi-agent ecosystem.
