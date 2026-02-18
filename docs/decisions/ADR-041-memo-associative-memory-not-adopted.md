# ADR-041 – MeMo (Associative Memory LMs) Evaluated and Not Adopted

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** None

---

## Context

[MeMo](https://arxiv.org/abs/2502.12851) (Zanzotto et al., Findings of ACL 2025) proposes replacing implicit memorization in transformer weights with explicit, transparent memorization through layered Correlation Matrix Memories (CMMs). The core thesis is "memorization precedes learning" — build a model that can perfectly store and retrieve token sequences first, then add generalization on top.

MeMo was evaluated because its principles — transparent memory, selective editing, algebraic forgetting — align philosophically with ctxt/dPKMS's sovereign-data, local-first values. The question is whether any technique or concept should be adopted.

### What MeMo Proposes

- **Correlation Matrix Memories (CMMs)** as storage primitive: tokens encoded as high-dimensional random vectors, stored via outer-product sums into matrices, retrieved via key projection
- **Hierarchical multi-layer CMMs:** h heads × l layers = h^l maximum sequence length (exponential capacity scaling)
- **Three operations:**
  - **Memorize:** Add outer product of key-value pair to matrix
  - **Retrieve:** Project query key through matrix layers to reconstruct value
  - **Forget:** Subtract the stored outer product — algebraically clean inverse of memorization
- **Penalizing matrix (Phi):** Distiller + inverse-frequency filter prevents storing duplicate patterns
- **Johnson-Lindenstrauss Transform (JLT):** Dimensionality reduction while preserving distances

### Evaluation Results

All experiments are **synthetic only** (random token sequences, vocabulary of 100K symbols):

- Storage capacity scales linearly with total parameters
- >0.97 accuracy with no decoys at d=4096/3 layers
- >0.88 with 40 decoy repeated tokens at d=8192/3 layers

**Not evaluated:** No perplexity on standard corpora. No comparison with transformers. No downstream NLU tasks. No natural language generation quality. Authors acknowledge HuggingFace compatibility issues prevented standard evaluation.

### Implementation

- Repository: [github.com/HumanCentricART/MeMo](https://github.com/HumanCentricART/MeMo) — Python/PyTorch
- 6 stars, 0 forks, 21 commits, 3 contributors
- **License: CC BY-NC-SA 4.0 (non-commercial)** — incompatible with commercial use
- Three implementations: raw PyTorch, nn.Module, HuggingFace wrapper (noted as having compatibility issues)
- No trained models on real text, no pip package, no documentation beyond README

---

## Decision

**MeMo will not be adopted as a dependency, technique, or architectural influence for ContextHelp. The research area (explicit memory architectures for LLMs) is noted for monitoring.**

---

## Rationale

### Wrong Level of Abstraction

MeMo operates at **token-level sequence storage and retrieval** — storing and reconstructing individual token sequences within correlation matrices. ctxt/dPKMS operates at the **knowledge-object level** — entities, decisions, tags, summaries, graph edges. There is no actionable bridge between these layers. Adopting CMMs would mean reimplementing at a level far below where ctxt adds value.

### Zero Evidence on Real Text

The paper is explicitly a **position paper** with synthetic-only evaluation. No natural language tasks, no perplexity benchmarks, no comparison with transformers of equivalent parameter count, no downstream task evaluation. Basing architectural decisions on unproven synthetic experiments would be premature.

### ctxt Already Achieves the Stated Benefits

MeMo's philosophical claims — transparent, editable, forgettable memory — are already addressed by ctxt's architecture through different, more mature mechanisms:

| MeMo Benefit | ctxt Equivalent |
|---|---|
| Transparent memory (inspect what's stored) | SQLite with structured schemas, explicit fields, full audit trail |
| Editable memory (modify stored knowledge) | Row updates, entity resolution, graph edge modification |
| Forgettable memory (selective removal) | Row deletion, FTS/vector index purge, attachment cleanup |
| Hierarchical organization | Pipeline stages (entity → summary → section → composition) |

ctxt's mechanisms are simpler, proven, and operate at the semantic level users and agents care about — not at the token level.

### Incompatible Stack and License

- **Python/PyTorch** research prototype vs. Go single binary (ADR-002)
- **CC BY-NC-SA 4.0** (non-commercial) — directly conflicts with context.help cloud monetization (ADR-032) and any commercial registry access
- Integration would require embedding Python/PyTorch or running a sidecar — neither aligns with zero-install philosophy (ADR-001)

### Research Maturity

6 stars, 0 forks, position paper classification, no community adoption, no downstream evaluations, no integration ecosystem. This is early-stage academic research, not production-viable technology.

### Team/Cloud Perspective (ADR-031, ADR-032, ADR-033)

MeMo's non-commercial license is a hard blocker for any deployment mode involving monetization. Even if the ideas were reimplemented independently, the token-level abstraction provides no value for team collaboration, RBAC, metered access, or registry monetization — all of which operate at the knowledge-object level.

### Alternatives Considered

| Option | Verdict |
|---|---|
| **Adopt CMMs for retrieval** | Rejected — vector search + FTS5 + graph traversal is more mature and proven |
| **Adopt algebraic forgetting** | Rejected — row deletion is simpler and more reliable; only relevant if ctxt had a neural memory layer |
| **Reference MeMo philosophy** | Noted — transparent/editable/forgettable memory aligns with ctxt values, but ctxt already implements these |
| **Monitor research area** | Accepted — explicit memory architectures (MeMo, MemOS, A-Mem) as a field |

---

## Consequences

### Positive

- No Python/PyTorch dependencies introduced
- No non-commercial license contamination
- Decision confirms ctxt's existing storage architecture (SQLite + structured schemas + graph) already provides the transparency, editability, and forgetability that MeMo targets at a lower level

### Negative

- None identified — MeMo provides no capability ctxt lacks

### Neutral / Considerations

- The explicit memory architecture research area (MeMo, MemOS, A-Mem, Mem0) is active and worth monitoring
- If a future paper in this lineage demonstrates real-language performance with a permissive license and a practical integration path, revisit

---

## Implementation Notes

No implementation is required. One concept is noted for future reference:

### Algebraic Forgetting (Monitor Only)

MeMo's subtraction-based forgetting — remove a stored memory by subtracting its exact outer-product representation — is mathematically elegant. The operation is O(1) per memory and exact when the stored representation is known.

This concept would only become relevant for ctxt if:
1. A neural memory layer were added for retrieval (not planned)
2. The neural layer needed precise, verifiable removal of specific content
3. A commercially-licensed implementation existed

Currently, knowledge-object-level deletion (row removal + index purge) is sufficient and more appropriate for ctxt's architecture.

---

## References

- ADR-001 – Local-First and Decentralized
- ADR-002 – Use Go
- ADR-013 – Knowledge Graph and Mentions
- ADR-022 – Vector Indexing and Hybrid Semantic Search
- ADR-031 – Nodes and context.help cloud Boundary
- ADR-032 – Entitlements and Metering as Policy Predicates
- [MeMo Paper (arXiv:2502.12851)](https://arxiv.org/abs/2502.12851)
- [MeMo at ACL Anthology (Findings of ACL 2025)](https://aclanthology.org/2025.findings-acl.785/)
- [MeMo GitHub Repository](https://github.com/HumanCentricART/MeMo)

---
