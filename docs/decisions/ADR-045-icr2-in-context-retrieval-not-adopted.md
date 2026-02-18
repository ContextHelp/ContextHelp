# ADR-045 – ICR²: In-Context Retrieval and Reasoning Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None
>
> **Reference:** [Eliciting In-context Retrieval and Reasoning for Long-context Large Language Models](https://arxiv.org/abs/2501.08248) (arXiv:2501.08248, Jan 2025; ACL 2025 Findings — Apple ML Research)

---

## Context

This paper (Qiu et al., 2025) proposes techniques for improving how long-context LLMs (LCLMs) handle retrieval and reasoning when entire knowledge bases are placed into the model's context window. It introduces:

1. **ICR² Benchmark** — a harder evaluation than LOFT, using strong retrievers (Contriever, DPR, BM25, BLINK, DrQA) to select confounding passages that realistically challenge models
2. **Retrieve-Then-Generate fine-tuning (SFT-RTA)** — training models to first identify relevant passages, then generate answers in a single forward pass
3. **Retrieval Attention Probing (RAP)** — an inference-time technique that identifies attention heads with high retrieval hit rates and uses them to filter context before generation
4. **Joint Retrieval Head Training** — adding a dedicated retrieval scoring layer alongside the generation head

Key results: Mistral-7B with SFT-RTA + RAP achieves +17 EM on LOFT and +13 EM on ICR² over vanilla RAG, competitive with GPT-4-Turbo despite being 7B parameters.

The question: **Should dPKMS or ctxt adopt any of these techniques to improve retrieval quality?**

---

## Decision

**Not adopted.** The paper solves a fundamentally different problem than what dPKMS + ctxt addresses.

---

## Rationale

### Problem Mismatch: In-Context vs. Explicit Retrieval

The paper's premise is that LCLMs can replace traditional RAG by processing entire knowledge bases in-context. dPKMS takes the opposite approach: explicit, structured retrieval (FTS5, vector search, graph traversal, RSQL) selects small, precise context before any LLM interaction.

```
Paper's architecture:
  Knowledge base → Stuff into 32K context → LLM retrieves + reasons → Answer

dPKMS architecture:
  Knowledge base → Multi-strategy retrieval (FTS5/vector/graph/SQL)
                 → Small, relevant context
                 → LLM generates with constrained output (LMQL/instructor)
                 → Answer
```

These are incompatible approaches. The paper optimizes the first; we build the second.

### Technique-by-Technique Assessment

| Technique | Requires | dPKMS Applicability |
|-----------|----------|---------------------|
| **SFT-RTA** | Fine-tuning Mistral-7B on 25K examples | ❌ We use LLMs via API, no fine-tuning |
| **RAP** | Long context stuffed into LLM | ❌ We don't stuff knowledge bases into context |
| **Joint Retrieval Head** | Custom model architecture changes | ❌ We don't modify model architectures |
| **ICR² Benchmark** | Evaluating LCLMs on retrieval tasks | ⚠️ Informational only (validates our choices) |

### The Paper Validates Our Approach

The most useful finding is negative: **existing LCLMs are highly sensitive to confounding passages**, with exact match dropping up to 51% when realistic distractors are present. This directly validates dPKMS's design choice of doing precision retrieval rather than relying on LLM attention:

- Our multi-strategy search (ADR-018) selects only relevant objects
- Focus profiles (ADR-020) further narrow context
- Constrained generation (ADR-014, LMQL) ensures structured output on small context

The paper's solution — fine-tune models to handle confounders better — is unnecessary when you avoid putting confounders in context in the first place.

### Revisit Conditions

Revisit this decision only if:

1. **dPKMS adds an "LLM-as-retriever" mode** where users dump entire collections into an LLM context for exploration (not planned)
2. **RAP becomes available as an API feature** from model providers (e.g., Anthropic/OpenAI expose attention-based filtering natively)
3. **Context windows grow to 1M+ tokens reliably** and the economics favor stuffing over structured retrieval (current trajectory suggests structured retrieval remains more cost-effective)

---

## Consequences

### Positive

- Confirms architectural soundness of explicit retrieval over in-context retrieval
- Avoids fine-tuning burden (25K training examples, GPU costs, model maintenance)
- Keeps system model-agnostic (no dependency on specific LLM internals)

### Negative

- None identified. The paper solves a problem we don't have.

### Neutral

- ICR² benchmark methodology (using strong retrievers for confounders) could inform how we test our own retrieval quality — specifically, evaluating whether our multi-strategy search degrades gracefully when results include near-miss documents.

---

## Comparison With Other Memory/Retrieval Research

| Research | Problem | Layer | Decision |
|----------|---------|-------|----------|
| **AMA (ADR-037)** | Multi-agent memory coordination | Application | ✅ Adopt (Phase 2-3) |
| **HEMA (ADR-040)** | Long-context conversation coherence | Application | ✅ Adopt (Phase 3) |
| **Taxonomy (ADR-042)** | Memory framework classification | Conceptual | ✅ Adopt partial |
| **BEAM/LIGHT (ADR-043)** | Long-term memory techniques | Application | ✅ Adopt partial |
| **ICR² (ADR-045)** | In-context LLM retrieval | Model internals | ❌ Not adopted |
| **MLPMemory (ADR-039)** | Single-agent inference speed | Model internals | ❌ Not adopted |
| **MeMo (ADR-041)** | Token-level associative memory | Model internals | ❌ Not adopted |

**Pattern:** Research operating at the model-internals layer (attention heads, fine-tuning, token association) is consistently not applicable to dPKMS, which operates at the application/knowledge layer. Research at the application layer (agent coordination, conversation coherence, memory frameworks) is consistently adoptable.

---

## References

- [Eliciting In-context Retrieval and Reasoning for Long-context LLMs](https://arxiv.org/abs/2501.08248) (Qiu et al., 2025)
- [ICR² Code & Data](https://github.com/apple/ml-icr2) (Apple ML Research)
- Related: ADR-018 (Multi-Strategy Search), ADR-020 (Focus Profiles), ADR-014 (Constrained Generation)
- Related rejections: ADR-039 (MLPMemory), ADR-041 (MeMo) — same "wrong layer" pattern
