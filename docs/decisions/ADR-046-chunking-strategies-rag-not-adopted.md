# ADR-046 – Advanced Chunking Strategies for RAG: Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None
>
> **Reference:** [Reconstructing Context: Evaluating Advanced Chunking Strategies for Retrieval-Augmented Generation](https://arxiv.org/abs/2504.19754) (arXiv:2504.19754, Apr 2025; ECIR 2025 Workshop on Knowledge-Enhanced IR — Merola & Singh)

---

## Context

This paper (Merola & Singh, 2025) compares two advanced chunking strategies designed to preserve global document context in RAG systems:

1. **Late chunking** — embed the full document at token level, then segment into chunks (preserving contextual embeddings before fragmentation)
2. **Contextual retrieval** — use an LLM (Phi-3.5-mini) to generate a contextual summary for each chunk, then embed the enriched chunk + apply hybrid search (dense + BM25 rank fusion at 4:1 weighting) + cross-encoder reranking (Jina Reranker V2)

Key results on NFCorpus:
- Contextual retrieval: NDCG@5 = 0.317 (Jina-V3)
- Late chunking: NDCG@5 = 0.309 (Jina-V3)
- Traditional early chunking: NDCG@5 = 0.312 (Jina-V3)
- Late chunking catastrophically fails on some models (BGE-M3: 0.070 vs 0.246 early)

The question: **Should dPKMS adopt either late chunking or contextual retrieval to improve search quality?**

---

## Decision

**Not adopted.** The chunking problem this paper addresses does not apply to dPKMS's architecture, and the findings that are relevant (rank fusion, model selection) are already accounted for.

---

## Rationale

### 1. dPKMS Does Not Chunk Documents

The paper's core premise is optimizing how documents are split into fixed-size or semantically-bounded chunks for embedding. dPKMS uses a fundamentally different approach: **structured decomposition into knowledge objects**.

```
Paper's architecture (chunking-based RAG):
  Document → Split into chunks (512 chars or semantic boundaries)
           → Embed each chunk independently
           → Retrieve chunks by similarity
           → Generate from retrieved chunks

dPKMS architecture (structured decomposition):
  Content → Ingestion pipeline
          → Knowledge object with:
             ├─ raw_content (full text)
             ├─ sections (structured decomposition)
             ├─ entities + mentions (knowledge graph)
             ├─ decisions, tasks (extracted metadata)
             ├─ summaries (multi-level)
             ├─ tags (classified)
             └─ embeddings (semantic vectors)
          → Multi-strategy retrieval (FTS5 + vector + graph + SQL)
          → Reranking (RRF merge)
```

Knowledge objects are first-class semantic units, not arbitrary fragments. Sections are decomposed by meaning (headings, logical blocks), not character count. This eliminates the context-loss problem that chunking introduces.

### 2. Marginal Gains, Massive Overhead

The paper's best contextual retrieval result improves NDCG@5 by:
- +2.6% over late chunking (0.317 vs 0.309)
- +1.6% over traditional early chunking (0.317 vs 0.312)

At the cost of:
- ~20GB VRAM for chunk contextualization
- LLM inference (Phi-3.5-mini) per chunk during indexing
- 4x slower indexing with dynamic segmentation models

This tradeoff is not justified for our use case, where structured decomposition already preserves context.

### 3. Rank Fusion Already Planned

The paper's most consistently useful finding is that hybrid search (dense + BM25 at 4:1 weighting) improves retrieval across all embedding models — up to 5.7x for weaker models (BGE-M3). dPKMS already implements multi-strategy search with Reciprocal Rank Fusion (RRF) merging FTS5, vector, graph, and metadata results.

### 4. Contextual Retrieval Already Adopted at the Right Layer

ADR-044 (ContextualRetriever) adopted a more sophisticated approach to context-aware retrieval — one that operates at the **conversation level** (understanding multi-turn user intent) rather than the **document level** (enriching chunks with summaries). The ContextualRetriever approach:
- Reduces failed retrievals by 49-67% (vs. this paper's ~2.6% NDCG gain)
- Adds no latency (vs. LLM inference per chunk)
- Solves a problem we actually have (conversational search context loss)

### 5. Model Sensitivity Reinforces Pluggable Design

The paper's finding that late chunking catastrophically fails on BGE-M3 (NDCG@5 drops from 0.246 to 0.070) while working adequately on Stella-V5 and Jina-V3 reinforces dPKMS's decision to support pluggable embedding backends (ADR-021). The right approach is model flexibility, not strategy lock-in.

### 6. Limited Evaluation Scope

The key comparison (contextual retrieval vs. late chunking) was evaluated on only 20% of NFCorpus (~50 queries, ~300 documents) due to GPU memory constraints. This is insufficient evidence to drive architectural changes.

---

## What We Learn (Without Adopting)

| Finding | Implication for dPKMS | Action |
|---------|----------------------|--------|
| Rank fusion consistently helps | Validates our RRF approach | None (already implemented) |
| Embedding model choice > chunking strategy | Validates pluggable backends | None (already designed) |
| Late chunking is unreliable across models | Avoid model-specific optimizations | Inform embedding model selection guidance |
| Contextual enrichment at index time helps | We already do this via enrichment pipelines | None (sections, summaries, entities already generated) |
| 4:1 dense-to-BM25 weighting optimal | Consider for RRF weight tuning | Minor: validate our RRF weights |

---

## Consequences

### Positive

- Confirms our structured decomposition approach avoids the chunking problem entirely
- Validates multi-strategy search with rank fusion
- Reinforces pluggable embedding model design

### Negative

- None identified. The paper solves a problem we don't have.

### Neutral

- The 4:1 dense-to-BM25 weighting finding is worth validating against our RRF weights during Phase 2-3 search optimization, though our multi-strategy approach (4+ strategies, not just 2) makes direct comparison imprecise.

---

## Comparison With Related Research

| Research | Problem | Our Situation | Decision |
|----------|---------|---------------|----------|
| **Chunking paper (ADR-046)** | Context loss in RAG chunks | We don't chunk; structured decomposition | ❌ Not adopted |
| **ContextualRetriever (ADR-044)** | Conversational search context loss | We have this problem (iterative search) | ✅ Adopt Phase 3 |
| **ICR² (ADR-045)** | In-context LLM retrieval | We do explicit retrieval, not in-context | ❌ Not adopted |
| **Taxonomy (ADR-042)** | Memory framework classification | Validates our text-based approach | ✅ Adopt partial |

**Pattern:** Papers solving RAG infrastructure problems (chunking, in-context retrieval) are not applicable because dPKMS's architecture already sidesteps these problems through structured knowledge objects and multi-strategy search. Papers solving application-layer problems (conversational context, memory management) are directly applicable.

---

## Revisit Conditions

Revisit only if:

1. **dPKMS adds a "raw document search" mode** that bypasses structured decomposition and searches over unprocessed document fragments
2. **Late chunking becomes a standard embedding model feature** (built-in, no overhead) and we need to index very large documents without decomposition
3. **The paper's results are replicated at scale** on larger, more diverse benchmarks with statistically significant improvements

---

## Related ADRs

- **ADR-044:** ContextualRetriever (adopted; solves the context problem at the right layer)
- **ADR-045:** ICR² (rejected; same "wrong layer" pattern)
- **ADR-042:** Cognitive Memory Taxonomy (adopted partial; validates multi-strategy search)
- **ADR-021:** Multi-Backend Storage (pluggable embedding models)
- **ADR-035:** Docling (optional document parsing; decomposition, not chunking)

---

## References

- [Reconstructing Context: Evaluating Advanced Chunking Strategies for RAG](https://arxiv.org/abs/2504.19754) (Merola & Singh, 2025)
- [ECIR 2025 Workshop on Knowledge-Enhanced IR](https://ecir2025.eu/)
- Related: ADR-044 (ContextualRetriever), ADR-045 (ICR²), ADR-042 (Taxonomy), ADR-021 (Multi-Backend)
