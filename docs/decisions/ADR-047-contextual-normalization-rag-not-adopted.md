# ADR-047 – Contextual Normalization for RAG Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None
>
> **Reference:** [Grounding Long-Context Reasoning with Contextual Normalization for Retrieval-Augmented Generation](https://arxiv.org/abs/2510.13191) (arXiv:2510.13191, Oct 2025; withdrawn from ICLR 2026)

---

## Context

This paper (Chen et al., 2025) demonstrates that surface-level formatting choices in RAG systems — delimiters, structural markers, whitespace patterns — significantly impact LLM accuracy even when semantic content is identical. The authors propose **Contextual Normalization (C-Norm)**, a training-free, inference-time technique that:

1. **Generates format candidates** by replacing whitespace with delimiters (-, _, :, ., ~, +, /, &) at a controlled ratio
2. **Scores candidates** using an Attention Balance Score (ABS) that measures how uniformly attention distributes across the context
3. **Selects the optimal delimiter** per model and applies it to all retrieved documents

Key findings:
- Switching delimiters can collapse accuracy from 81% to 10% (LLaMA-2-7B-Chat, ampersand vs. hyphen)
- Optimal delimiters are model-specific (LLaMA prefers ".", Qwen prefers "-")
- Tokenizer design (BPE vs. SentencePiece) correlates with delimiter sensitivity (Pearson r = -0.82 for token count vs. accuracy in SentencePiece)
- Modest gains on LongBench-v2: 1-3% accuracy improvement across models

The paper was submitted to ICLR 2026 but **withdrawn by the authors** before review completion.

The question: **Should dPKMS or ctxt adopt C-Norm to improve context presentation to LLMs?**

---

## Decision

**Not adopted.** The problem C-Norm solves does not exist in dPKMS's architecture.

---

## Rationale

### Problem Mismatch: Raw Passages vs. Structured Knowledge

C-Norm addresses a problem in traditional RAG pipelines where retrieved document chunks are concatenated with arbitrary formatting before being stuffed into an LLM prompt. dPKMS doesn't work this way:

```
Traditional RAG (paper's assumption):
  Query → Retrieve top-k passages → Concatenate with delimiters → LLM prompt
  ↳ Delimiter choice matters here

dPKMS architecture:
  Query → Multi-strategy retrieval (FTS5/vector/graph/RSQL)
        → Structured objects (sections, entities, mentions)
        → Template-based composition (ADR-017)
        → Constrained generation (LMQL/instructor)
  ↳ We control format end-to-end; no arbitrary delimiters
```

### Technique Assessment

| Aspect | C-Norm | dPKMS Relevance |
|--------|--------|-----------------|
| **Delimiter optimization** | Selects optimal delimiter per model | ❌ We use structured templates, not raw concatenation |
| **ABS scoring** | Measures attention uniformity via forward pass | ❌ Requires model attention access; we use LLMs via API |
| **Format standardization** | Normalizes inconsistent retrieved content | ⚠️ Mildly relevant — but our atomic sections (ADR-028) already produce consistent format |
| **Model-specific tuning** | Different models need different delimiters | ❌ We're model-agnostic by design (ADR-005) |

### Why the Insight Is Real but Irrelevant to Us

The paper's core finding — that formatting affects LLM attention patterns — is empirically valid and useful to know. However:

1. **We already produce well-structured context.** Atomic sections (ADR-028), structured entities (ADR-029), and template-based composition (ADR-017) ensure consistent, clean context formatting.
2. **We don't concatenate raw passages.** Our retrieval returns structured objects with defined schemas, not arbitrary text chunks.
3. **Attention access requires model internals.** ABS scoring needs attention weights from the final transformer layer — unavailable via standard API endpoints (OpenAI, Anthropic, Ollama).
4. **Gains are marginal.** 1-3% on LongBench-v2 with small models (7B, 1.5B) doesn't justify adding complexity.

### Paper Quality Concerns

- **Withdrawn from ICLR 2026** — authors pulled the submission before review completion
- **Tested only on small models** (LLaMA-2-7B, Qwen2.5-1.5B) — unclear if findings generalize to production-scale models (70B+, GPT-4, Claude)
- **Instruction-tuned models show smaller gains** — suggesting the problem diminishes with better-aligned models
- **No runtime overhead reported** — computational cost of ABS scoring unquantified

### Useful Takeaways (Not Requiring Adoption)

1. **Prompt template hygiene matters.** When crafting LLM prompts (composition, enrichment), avoid exotic delimiters. Prefer clean whitespace and standard punctuation.
2. **Test prompt formats across models.** If switching AI providers (ADR-005), verify that prompt templates perform consistently.
3. **Tokenizer-aware formatting.** Models with SentencePiece tokenizers are more sensitive to delimiter choice than BPE-based models.

### Revisit Conditions

Revisit only if:

1. **dPKMS adds a "raw RAG" mode** that concatenates unstructured passages directly (not planned)
2. **API providers expose attention weights** as a standard feature, making ABS scoring practical
3. **A peer-reviewed version** appears with results on production-scale models showing >10% gains

---

## Consequences

### Positive

- Avoids unnecessary complexity for a problem we don't have
- Validates our structured-context approach (formatting matters → so control it architecturally)
- Adds empirical evidence for prompt template hygiene as a best practice

### Negative

- None identified. The paper solves a problem outside our architecture.

### Neutral

- The delimiter sensitivity data (81% → 10% accuracy collapse) is a useful reference when documenting best practices for plugin developers building custom prompt templates.

---

## Comparison With Other Retrieval/RAG Research

| Research | Problem | Layer | Decision |
|----------|---------|-------|----------|
| **ContextualRetriever (ADR-044)** | Multi-turn conversational search | Application | ✅ Adopt (Phase 3) |
| **BEAM/LIGHT (ADR-043)** | Long-term memory techniques | Application | ✅ Adopt partial |
| **ICR² (ADR-045)** | In-context LLM retrieval | Model internals | ❌ Not adopted |
| **Chunking Strategies (ADR-046)** | Document chunking for RAG | Inapplicable | ❌ Not adopted |
| **C-Norm (ADR-047)** | RAG context formatting | Prompt engineering | ❌ Not adopted |

**Pattern:** Papers optimizing traditional RAG pipelines (chunk → concatenate → prompt) address a problem dPKMS solved architecturally through structured decomposition and template-based composition. Research at the application layer (conversational search, memory techniques) remains consistently adoptable.

---

## References

- [Grounding Long-Context Reasoning with Contextual Normalization for RAG](https://arxiv.org/abs/2510.13191) (Chen et al., 2025)
- Related: ADR-017 (Composition Engine), ADR-028 (Atomic Notes), ADR-005 (Decorator Pattern for AI Providers)
- Related rejections: ADR-045 (ICR²), ADR-046 (Chunking Strategies) — same "wrong architecture" pattern
