# ADR-043 – BEAM/LIGHT: Long-Term Memory Techniques Partially Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

[BEAM/LIGHT](https://arxiv.org/abs/2510.27246) (Tavakoli et al., ICLR 2026) presents two contributions: BEAM, a benchmark evaluating 10 cognitive memory abilities in LLMs across 100K–10M token conversations, and LIGHT, a three-component memory enhancement framework (episodic memory, working memory, scratchpad) that augments any LLM with long-term conversational memory.

BEAM/LIGHT was evaluated because it addresses the same problem space as ctxt's retrieval and surfacing systems — how to effectively store, retrieve, and use accumulated knowledge at scale — and because it provides empirically validated techniques with specific ablation results.

### What BEAM/LIGHT Does

**BEAM (Benchmark):**
- 100 synthetic conversations (100K to 10M tokens) with 2,000 human-validated probing questions
- 10 memory abilities: abstention, contradiction resolution, event ordering, information extraction, instruction following, information update, multi-hop reasoning, preference following, summarization, temporal reasoning
- Nugget-based evaluation (atomic criteria scored 0/0.5/1)
- Kendall tau-b for event ordering

**LIGHT (Framework):**
- **Episodic memory:** Key-value pairs extracted per conversation turn, embedded via bge-small-en-v1.5, stored in FAISS, retrieved via top-k nearest neighbors
- **Working memory:** Last z dialogue pairs (recent context window)
- **Scratchpad:** Iteratively accumulated salient facts across four categories (semantic knowledge, autobiographical details, prospective memory, contextual metadata), compressed from 30K to 15K tokens when threshold exceeded, with per-query relevance filtering via semantic chunking + binary assessment

### Key Results

| Context Length | Improvement over Vanilla LLM |
|---|---|
| 100K tokens | +44–49% |
| 500K tokens | +73% |
| 1M tokens | +60–76% |
| 10M tokens | +107–156% |

**Ablation at 10M tokens (impact of removing each component):**
- Remove retrieval: -8.5%
- Remove noise filtering: -8.3%
- Remove working memory: -5.7%
- Remove scratchpad: -3.7%

**Retrieval budget:** k=15 optimal. k=20 degrades (noise). k=5 is 6-8% worse.

**Critical finding:** Contradiction resolution scores 0–5% across ALL methods (vanilla, RAG, LIGHT) with ALL models tested.

### Tech Stack

- Python, VLLM, FAISS, BAAI/bge-small-en-v1.5 embeddings
- Qwen2.5-32B-AWQ for extraction, filtering, and scratchpad updates
- GPT-4.1-nano for scratchpad compression
- LangChain SemanticChunker for scratchpad segmentation
- **License:** MIT (code), CC BY-SA 4.0 (data)
- **Venue:** ICLR 2026 (top-tier, peer-reviewed)

---

## Decision

**Three specific techniques from LIGHT are adopted for ctxt's retrieval, surfacing, and conversation coherence systems. The full LIGHT system is not adopted due to computational requirements and stack incompatibility.**

---

## Rationale

### Why Partially Adopt

BEAM/LIGHT provides the strongest empirical evidence of any paper evaluated in this ADR series. Unlike MeMo (synthetic-only, position paper), MLPMemory (orthogonal problem), or HEMA (smaller scale), BEAM/LIGHT:

- Evaluates across 100K–10M tokens with rigorous ablation
- Identifies specific technique contributions with quantified impact
- Is accepted at a top-tier venue (ICLR 2026)
- Uses MIT license for code
- Provides techniques that map directly to ctxt's existing architecture without requiring the full system

The three adopted techniques are **implementation patterns**, not system dependencies. They can be implemented in Go using ctxt's existing plugin and pipeline architecture without introducing Python, VLLM, or GPU requirements.

### Why Not the Full System

1. **Computational requirements.** LIGHT requires Qwen2.5-32B running alongside the primary model for every conversation turn (key-value extraction, scratchpad updates, relevance filtering). This is fundamentally incompatible with ctxt's local-first, single-binary, runs-on-laptop philosophy (ADR-001, ADR-002).

2. **Stack incompatibility.** Python + VLLM + FAISS + GPU inference. ctxt is Go with SQLite default.

3. **Single-agent only.** LIGHT handles single-user conversations. ctxt needs multi-agent coordination (ADR-037) which LIGHT does not address.

4. **Conversation-turn granularity.** LIGHT operates per dialogue turn. ctxt operates per knowledge object at ingestion granularity. The abstraction levels are different.

### Relation to Existing ADRs

| ADR | Relation |
|---|---|
| **ADR-040 (HEMA)** | LIGHT scratchpad supersedes HEMA compact memory as the preferred design for conversation coherence. ADR-040 implementation should use the LIGHT scratchpad design instead. |
| **ADR-042 (Cognitive Memory Taxonomy)** | BEAM empirically confirms contradiction resolution is unsolved (0–5% across all methods). Validates Gap 1 identification and calibrates expectations. |
| **ADR-037 (Agent Coordination)** | No overlap — LIGHT is single-agent only. |
| **ADR-022 (Hybrid Semantic Search)** | Per-query noise filtering extends the reranking strategy. |
| **ADR-011 (Multi-Source Reranker)** | Noise filtering can be a reranker step. |

### Team/Cloud Perspective (ADR-031, ADR-032, ADR-033)

The three adopted techniques are relevant across all deployment modes:

- **Solo local:** Noise filtering runs as a lightweight constrained-generation step using whatever AI provider is configured (could be a local model with LMQL `in("yes", "no")`)
- **Team nodes:** Same techniques apply when multiple team members query shared knowledge
- **Monetized access:** Per-query relevance filtering is especially important for "rent access to know-how" scenarios — consumers paying for query results should get high-relevance responses, not noisy retrieval dumps

---

## Consequences

### Positive

- Empirically validated improvements to retrieval quality (+8.3% from noise filtering alone)
- Scratchpad design provides a concrete, proven architecture for ADR-040 conversation coherence
- k=15 retrieval budget provides an evidence-based configuration default
- BEAM's 10-ability taxonomy provides evaluation framework for ctxt's search quality
- Contradiction resolution confirmed as open problem — validates ADR-042 Gap 1 and adjusts expectations

### Negative

- Per-query noise filtering adds latency (one LLM call per retrieval, though it can use a small/local model)
- Scratchpad design adds implementation complexity beyond HEMA's simpler compact memory
- Two additional pipeline steps to build and maintain (noise filter, scratchpad)

### Neutral / Considerations

- The noise filtering technique pairs well with constrained generation — binary relevance can be enforced via LMQL `in("yes", "no")` for local models, making it deterministic rather than probabilistic
- Scratchpad compression ratio (30K→15K, 50% reduction) provides a concrete engineering target

---

## Implementation Notes

### Technique 1: Per-Query Noise Filtering (Phase 3, Medium Priority)

After multi-strategy retrieval (ADR-022) and before final ranking (ADR-011), add a relevance filtering step:

1. Semantic-chunk each candidate result (using section boundaries from ADR-028 atomic decomposition)
2. For each chunk, assess binary relevance to the query
3. Discard irrelevant chunks before final ranking

```go
// RelevanceFilter is a post-retrieval, pre-ranking step that removes
// noise from candidate results. Pluggable via AI provider.
type RelevanceFilter interface {
    FilterByRelevance(ctx context.Context, query string, candidates []SearchResult) ([]SearchResult, error)
}
```

For constrained generation compatibility:
- Local models: LMQL `in("yes", "no")` per chunk — deterministic, no post-processing
- API models: instructor with `bool` field — Pydantic retry for schema enforcement

This complements static focus profile filtering (ADR-015) with dynamic, per-query filtering.

### Technique 2: LIGHT Scratchpad as ADR-040 Implementation Design (Phase 3)

When implementing ADR-040 (HEMA conversation coherence), use the LIGHT scratchpad design instead of HEMA's simpler compact memory:

**Four memory categories** (instead of single narrative):
- **Semantic knowledge:** Domain facts, definitions, relationships
- **Autobiographical details:** User-specific information, preferences, history
- **Prospective memory:** Future intentions, scheduled actions, commitments
- **Contextual metadata:** Session state, topic threads, conversation flow

**Compression with threshold:**
- Accumulate facts until 30K token threshold
- Compress to 15K tokens via LLM summarization (using configured AI provider)
- At inference time, semantic-chunk the scratchpad and filter by relevance (reusing Technique 1)

**Integration with ctxt:** The scratchpad is stored as a knowledge object with `type: scratchpad`, `subtype: conversation.context`, updated after each interaction. Focus profiles (ADR-015) determine which memory categories are prioritized.

### Technique 3: Retrieval Budget k=15 Default (Immediate, Low Effort)

Set k=15 as the default for vector search top-k retrieval in configuration documentation and code defaults. Paper's ablation shows:
- k=5: 6-8% worse (insufficient recall)
- k=15: optimal across all context lengths
- k=20: degrades (noise overwhelms signal)

Users can override via configuration, but k=15 is the empirically justified default.

### Reference: BEAM 10-Ability Evaluation Framework

When building evaluation infrastructure for ctxt's search and composition quality, reference BEAM's ability decomposition:

1. Abstention (knowing when NOT to answer)
2. Contradiction Resolution (detecting conflicting information)
3. Event Ordering (temporal sequence reconstruction)
4. Information Extraction (pulling specific facts)
5. Instruction Following (honoring standing directives)
6. Information Update (incorporating new information)
7. Multi-hop Reasoning (connecting multiple knowledge objects)
8. Preference Following (respecting user preferences)
9. Summarization (condensing knowledge)
10. Temporal Reasoning (time-relative queries)

This maps well to ctxt's capabilities and provides more granular evaluation than standard IR metrics (precision, recall, MRR).

---

## References

- ADR-011 – Multi-Source Reranker
- ADR-015 – Focus Profiles
- ADR-022 – Vector Indexing and Hybrid Semantic Search
- ADR-028 – Atomic Notes and Decomposition Strategy
- ADR-031 – Nodes and context.help cloud Boundary
- ADR-032 – Entitlements and Metering as Policy Predicates
- ADR-037 – Agent-Aware Adaptive Memory
- ADR-040 – HEMA Long-Context Conversation Coherence
- ADR-042 – Cognitive Memory in LLMs: Taxonomy Evaluation
- [BEAM/LIGHT Paper (arXiv:2510.27246)](https://arxiv.org/abs/2510.27246)
- [BEAM GitHub Repository (MIT)](https://github.com/mohammadtavakoli78/BEAM)

---
