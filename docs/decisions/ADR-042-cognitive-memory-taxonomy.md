# ADR-042 – Cognitive Memory in LLMs: Taxonomy Evaluation and Applicability

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** —
> **Superseded by:** —
>
> **Reference:** [Cognitive Memory in Large Language Models](https://arxiv.org/abs/2504.02441) (arXiv:2504.02441, Apr 2025)

---

## Context

Recent memory research (Apr 2025) published a comprehensive taxonomy of memory mechanisms in LLMs. Rather than proposing a novel technique, this paper catalogs four distinct memory approaches and their tradeoffs. The question: **Does this taxonomy provide actionable guidance for dPKMS + ctxt, or is it purely conceptual?**

To answer, we must evaluate:
1. Whether the taxonomy's categories align with our architecture
2. If the use-case recommendations match our scenarios
3. Whether the paper identifies gaps we should address
4. Whether adopting the taxonomy helps us make better decisions

---

## The Paper: Four Memory Approaches

The taxonomy categorizes memory in LLMs into four orthogonal implementations:

### Approach 1: Text-Based Memory

**What it is:** Stores information as natural language in external systems (databases, vectors, knowledge graphs).

**Acquisition methods:**
- **Full retention:** Store all conversation history as text
- **Summarization:** Condense to key facts (e.g., COMEDY, Memory Bank)
- **Hybrid:** Mix selection + summarization

**Management:**
- Update strategies: LRU caching, forgetting curves, rolling summaries
- Access: Role-based, task-based, autonomous retrieval
- Storage: Trees (HAT), hash tables, vectors, knowledge graphs
- Conflict resolution: Memory resolution, disambiguation, retention

**Retrieval:**
- Full-text search, SQL queries, semantic search, tree traversal, hash lookup, multi-pass

**Strengths:**
- Explicit and human-interpretable
- Flexible querying (SQL, semantic, full-text)
- Easy to update dynamically
- Supports complex reasoning over structured data

**Limitations:**
- Computational overhead for large corpora
- Scalability challenges (searching large databases)
- Requires careful conflict resolution
- Latency for external lookup

**Use case recommendations (from paper):**
> "Suitable for broad queries or when the exact location or label of information is unknown... best for multi-turn dialogue with explicit context needs"

---

### Approach 2: KV Cache-Based Memory

**What it is:** Optimizes key-value pairs in transformer attention mechanisms during inference.

**Selection strategies (9 categories):**
- **Regularity-based** (StreamingLLM, Dynamic Discriminative Operations): Keep tokens with high attention patterns
- **Value-aware scoring** (attention scores + value norms): Preserve high-impact tokens
- **Score-based** (A2SF, Query-aware): Compute importance dynamically
- **Special tokens** (RazorAttention, compensation tokens): Mark important positions
- **Learning-based:** Train selector networks
- **Layer/head-specific:** Differentiate by attention head
- **Locality-sensitive hashing:** Approximate nearest neighbors
- **Backtracking:** Maintain multiple history versions

**Compression techniques:**
- Low-rank decomposition
- KV merging (combine similar tokens)
- Multi-level compression (hierarchical storage)

**Management:**
- Offload to cheaper memory (CPU, disk)
- OS-level integration
- Shared attention mechanisms

**Key observations from literature:**
- "Heavy Hitters Oracle (H2O): a small subset of tokens contributes majority of values"
- "Keyformer: ~90% of attention weights concentrate on specific token subsets"
- "RazorAttention: most attention heads focus on local context"

**Strengths:**
- Native to transformer architecture
- Low inference latency
- Automatic during model execution
- Enables long-context inference

**Limitations:**
- Information loss during pruning/compression
- Token budget constraints
- Model architecture dependent
- Not interpretable (black-box)

**Use case recommendations (from paper):**
> "Optimal for real-time inference with long contexts... recommended when memory bandwidth is constrained... effective for streaming applications"

---

### Approach 3: Parameter-Based Memory

**What it is:** Encodes memories directly into model weights through adaptation.

**Techniques:**
- **LoRA (Low-Rank Adaptation):** Add trainable low-rank modules
- **Test-Time Training (TTT):** Update parameters during inference
- **Mixture of Experts (MoE):** Route through specialized modules

**Strengths:**
- Compact storage (low-rank, no external databases)
- Parameter efficient
- Knowledge integrated directly in model
- No external lookup needed

**Limitations:**
- Requires training overhead
- Limited to training data + fine-tuning
- Not adaptable post-deployment without retraining
- Knowledge becomes frozen again after training

**Use case recommendations (from paper):**
> "Recommended for specialized domains requiring persistent adaptation... suitable when fine-tuning resources are available"

---

### Approach 4: Hidden-State-Based Memory

**What it is:** Maintains state across sequences using RNN-inspired mechanisms in transformers.

**Techniques:**
- **Chunk mechanisms:** Divide text into processable segments
- **Recurrent Transformers:** Pass states between chunks
- **Mamba architecture:** State-space models for efficient long-context processing

**Strengths:**
- Efficient for streaming/long sequences
- Captures semantic relationships
- Low memory footprint per token
- Can process infinite sequences

**Limitations:**
- Less interpretable than text-based
- Complex to implement
- Limited to specific architectures (Mamba, recurrent-enabled)
- State explosion with very long contexts

**Use case recommendations (from paper):**
> "Ideal for infinitely long stream conversations... best for tasks requiring efficient sequential processing"

---

## Mapping to dPKMS + ctxt

### Current Architecture Analysis

**dPKMS today:**
```
Storage layer (SQLite/PostgreSQL)
├─ Knowledge graph (entities, mentions)
├─ Full-text index (FTS5)
├─ Vector store (pgvector/LEANN)
└─ Attachment store (documents, images)

Query engine:
├─ FTS strategy (keyword matching)
├─ Vector strategy (semantic similarity)
├─ Graph strategy (entity relationships)
├─ Metadata strategy (SQL queries)
└─ Registry strategy (federated search)
```

**What we're doing:**
- **Primarily: Text-based memory** (storing as knowledge objects with metadata)
- **Supplementary: Vector-based retrieval** (semantic search layer)
- **Supplementary: Graph-based traversal** (entity relationships)

**What we're NOT doing:**
- KV cache optimization (we don't run LLMs for inference)
- Parameter-based memory (we don't train/adapt models)
- Hidden-state tracking (we don't maintain state machines)

### Taxonomy Alignment: Where We Fit

```
Paper's Taxonomy:
┌─────────────────────────────────────────┐
│ Text-Based Memory (Explicit storage)   │
│ ✅ This is mostly what dPKMS does      │
│                                         │
│ ├─ Acquisition: Selection + Summariz.  │
│ │  ✅ Pipelines select/summarize       │
│ │                                       │
│ ├─ Management: Updates, access, store  │
│ │  ✅ Job system manages updates       │
│ │  ✅ Focus profiles control access    │
│ │  ✅ Multi-backend storage            │
│ │                                       │
│ ├─ Retrieval: Full-text, SQL, semantic │
│ │  ✅ FTS5, vector, SQL, graph        │
│ │                                       │
│ └─ Strengths: Explicit, flexible       │
│    ✅ We leverage this                 │
│                                         │
├─ KV Cache (Transformer optimization)   │
│ ❌ Not applicable (no inference)       │
│                                         │
├─ Parameters (Model adaptation)         │
│ ❌ Not applicable (no fine-tuning)     │
│                                         │
└─ Hidden-State (Sequential state)       │
  ⚠️ Partially relevant (conversations)  │
  See ADR-040 (HEMA) for this           │
```

---

## Deep Analysis: Text-Based Memory Within Our System

### What the Taxonomy Teaches About Text-Based Approach

The paper's breakdown of text-based memory into acquisition/management/retrieval is valuable because it helps us evaluate **whether we're doing all three components well**.

#### Component 1: Acquisition (Selection + Summarization)

**Paper's insight:**
- Pure selection: Keep all data (no information loss, high storage)
- Pure summarization: Condense to essentials (low storage, possible loss)
- Hybrid: Combine both (balanced)

**Our current approach:**
```
Ingestion pipeline:
├─ Full text captured (selection: keep all)
├─ Sections extracted (structured decomposition)
├─ Entities extracted (knowledge graph)
├─ Mentions extracted (relationships)
├─ Summary generated (OPTIONAL enrichment)
└─ Tags assigned (semantic classification)
```

**Assessment:** We do **hybrid acquisition** (full + structured + optional summary).

**Gap identified by taxonomy:** We generate summaries as optional enrichment, not mandatory. But for long documents, mandatory summarization would be better for retrieval efficiency.

**Implication for dPKMS:**
- Consider making summary generation mandatory in Phase 2-3 enrichment
- Summaries should be stored alongside full content for comparison
- Different summary levels: one-line (compact), paragraph (context), full (reference)

---

#### Component 2: Management (Update, Access, Storage, Conflict Resolution)

**Paper's breakdown:**

| Aspect | Technique | dPKMS Status |
|--------|-----------|--------------|
| **Updates** | LRU, forgetting curves, rolling | ✅ Job system handles |
| **Access control** | Role-based, task-based, autonomous | ✅ Focus profiles handle |
| **Storage structure** | Trees, tables, vectors, graphs | ✅ Multi-backend support |
| **Conflict resolution** | Memory resolution, disambiguation | ⚠️ Partially via entity resolution |

**Gap identified:** No explicit "conflict resolution" mechanism when:
- Same entity mentioned with different values (old vs. new info)
- Contradictory statements about same topic
- Versioned knowledge (decision changed, but history lost)

**Example problem:**
```
User captures: "Decision: Use PostgreSQL"
Later captures: "Decision changed: Use MySQL"

Current dPKMS:
├─ Both stored as separate objects
├─ No automatic conflict detection
└─ User must explicitly search for contradictions

With taxonomy awareness:
├─ Detect: "Both are decisions about database choice"
├─ Flag as: "Contradictory/superseded"
├─ Version: "Decision v1 (Jan 15) → v2 (Feb 18)"
└─ Enable: "Show me the decision evolution"
```

**Implication for dPKMS:**
- Implement "contradiction detection" in enrichment pipeline (Phase 3+)
- Add versioning metadata (supersedes, related-to, contradicts)
- Create "knowledge evolution" tracking

---

#### Component 3: Retrieval (Full-text, SQL, Semantic, Traversal, Multi-pass)

**Paper's breakdown:**

| Strategy | Implementation | dPKMS Status |
|----------|---|---|
| **Full-text search** | Keyword matching in content | ✅ FTS5 |
| **SQL queries** | Structured metadata filtering | ✅ SQL on properties |
| **Semantic search** | Vector similarity | ✅ pgvector/LEANN |
| **Tree traversal** | Entity graph navigation | ✅ Graph traversal |
| **Hash-based lookup** | Fast key retrieval | ✅ ID lookups |
| **Multi-pass search** | Cascade queries (refine results) | ⚠️ User must refine manually |

**Gap identified:** No built-in "multi-pass" or "refinement cascade" retrieval.

**Example problem:**
```
User: "Show me decisions about architecture"
Result: [D1, D2, D3, D4] (4 results)

User: "From those, show only high-impact"
Current: Must re-query with added filters
  Query: "decisions about architecture AND high-impact"

With multi-pass retrieval:
├─ Pass 1: Find architecture decisions
├─ Pass 2: Filter to high-impact
├─ Pass 3: Rank by recency
└─ Return refined subset efficiently
```

**Implication for dPKMS:**
- Build "query refinement" as first-class feature
- Store intermediate results (pass 1) for faster pass 2
- Cache common refinement patterns
- Integrate with conversation coherence (ADR-040 HEMA)

---

### What the Taxonomy Teaches About Our Limitations

**Key insight from paper:**
> "LLMs are static after training... once trained, knowledge remains fixed until retraining"

**What this means for dPKMS:**
- We support dynamic updates (real-time ingestion) ✅
- But we don't support "forgetting" or "intelligent pruning"
- Knowledge grows unbounded (storage cost increases)
- No cognitive efficiency (keeping irrelevant old data)

**Paper recommends:** "LRU caching, forgetting curves"

**Missing from dPKMS:**
- Automatic archival of old knowledge (beyond retention policies in ADR-037)
- "Heat-based" retrieval (frequently accessed vs. stale)
- Cognitive efficiency: keep "hot" data active, "cold" data archived

---

## Does the Taxonomy Provide Actionable Guidance?

### Question 1: Does it change our architecture decisions?

**No.** The taxonomy confirms we're on the right path:
- We're correctly using text-based memory (explicit, interpretable)
- We're correctly using multi-strategy retrieval
- We're correctly avoiding KV-cache and parameter-based (not applicable)

### Question 2: Does it reveal gaps we should address?

**Yes, three gaps:**

**Gap 1: Conflict Resolution (Medium Priority)**
- Problem: Contradictory information not flagged
- Solution: Add "contradiction detection" enrichment step
- Timing: Phase 3 (after ADR-037 agent coordination)
- Benefit: Better knowledge integrity, auditable evolution

**Gap 2: Multi-Pass Retrieval (Medium Priority)**
- Problem: Refinement requires manual re-querying
- Solution: Build query refinement cascade
- Timing: Phase 3 (after core search optimization)
- Benefit: Better UX for iterative search (ties to HEMA ADR-040)

**Gap 3: Cognitive Efficiency / Forgetting (Low Priority)**
- Problem: Knowledge grows unbounded; no pruning
- Solution: Implement selective archival + heat-based retrieval
- Timing: Phase 4 (operational optimization)
- Benefit: Reduced storage costs, faster searches on "active" knowledge

### Question 3: Does adopting the taxonomy help us make better decisions?

**Yes, in two ways:**

1. **Validates current approach:** Confirms text-based + multi-strategy is correct for our use case
2. **Clarifies roadmap gaps:** Identifies three missing capabilities aligned with our growth

### Question 4: Should we adopt the taxonomy as a formal framework?

**Partially yes.** We should adopt:
- The three-component breakdown (acquisition, management, retrieval) as our internal model
- The use-case recommendations as validation
- The identification of text-based as our primary approach

But we should **not** try to support all four approaches (KV-cache, parameters, hidden-state) as they're not applicable.

---

## Comparison: Where Taxonomy Fits With Other Memory Research

| Research | Type | Problem | Our Adoption |
|----------|------|---------|--------------|
| **AMA (ADR-037)** | Novel approach | Multi-agent coordination | ✅ Yes (Phase 2-3) |
| **HEMA (ADR-040)** | Novel approach | Long-context coherence | ✅ Yes (Phase 3) |
| **MLPMemory (ADR-039)** | Novel technique | Single-agent inference | ❌ No (wrong layer) |
| **SuperMemory (ADR-038)** | Product | Cloud agent memory | ❌ No (wrong arch) |
| **Taxonomy (ADR-042)** | Classification | Understanding approaches | ✅ Partial (validates + guides) |

---

## Decision: Adopt Taxonomy as Internal Framework

### What We Adopt

1. **Mental model:** Text-based memory has three components (acquisition, management, retrieval)
2. **Self-assessment:** We're strong on retrieval, adequate on management, weak on acquisition
3. **Roadmap alignment:** Three identified gaps map to Phase 3-4 work
4. **Use-case validation:** Confirms text-based is right for our scenarios

### How We Integrate

**In architecture docs:**
- Reference taxonomy as framework (docs/dpkms/memory-systems-framework.md)
- Show how each dPKMS component maps to taxonomy
- Document the three gaps with phase assignments

**In ADRs:**
- Cross-reference when discussing memory improvements
- Use taxonomy terminology (acquisition, management, retrieval)
- Refer to use-case recommendations

**In roadmap:**
- Phase 3 work includes two gap closures: conflict resolution, multi-pass retrieval
- Phase 4 work includes cognitive efficiency
- These are prioritized relative to agent coordination (ADR-037) and conversation coherence (ADR-040)

### What We Don't Adopt

- ❌ KV-cache optimization (not applicable; we don't run inference)
- ❌ Parameter-based adaptation (not applicable; we don't fine-tune)
- ❌ Hidden-state tracking (handled separately by HEMA ADR-040)

---

## Three Gaps Identified: Detailed Implementation Plan

### Gap 1: Conflict Resolution & Knowledge Evolution (Phase 3)

**Problem:** When knowledge contradicts (old decision vs. new decision), system doesn't detect or flag.

**Proposal:**

```yaml
conflict_detection:
  During enrichment:
    1. Extract decision/fact from new object
    2. Search knowledge graph for related prior objects
    3. Compare: Are they about same topic?
    4. If yes:
       - If values same: Link as "corroborates"
       - If values differ: Flag as "contradicts"
       - If superseding: Mark as "supersedes"

  Storage:
    - decision object stores:
      - value: "Use PostgreSQL"
      - decision_date: "2025-01-15"
      - status: "OPEN"

    - When new decision found:
      - related_decisions: [{id, type: "supersedes"}]
      - evolution_chain: [old_id → new_id]

  Retrieval:
    - Query: "Show me decision evolution about database choice"
    - System traces: v1 (MySQL) → v2 (PostgreSQL) → v3 (MySQL again)
    - Shows: rationale for each change, decision dates, stakeholders involved
```

**Benefit:** Users can audit knowledge evolution; reduces confusion about current state.

**Effort:** Medium (requires enrichment logic + schema changes)

---

### Gap 2: Multi-Pass Refinement (Phase 3)

**Problem:** Iterative search requires user to re-specify full context each turn.

**Proposal:**

```yaml
refinement_cascade:
  Turn 1: User: "Show me decisions about architecture"
    - Pass 1: search(type==decision, topic==architecture)
    - Result: [D1, D2, D3, D4]
    - Cache: pass_1_results

  Turn 2: User: "From those, only high-impact"
    - Pass 2: filter(pass_1_results, impact==HIGH)
    - Result: [D1, D3]
    - Cache: pass_2_results

  Turn 3: User: "Most recent one first"
    - Pass 3: sort(pass_2_results, by=date DESC)
    - Result: [D3, D1]
    - Cache: pass_3_results (final)

  Benefit:
    - No re-searching for full dataset each time
    - Faster convergence to final results
    - Enables conversation threading (ties to HEMA ADR-040)
```

**Benefit:** Better UX for iterative exploration; faster response times.

**Effort:** Medium (requires query state management + caching)

---

### Gap 3: Cognitive Efficiency / Heat-Based Retrieval (Phase 4)

**Problem:** Knowledge grows unbounded; searching old archives is slow.

**Proposal:**

```yaml
heat_based_retrieval:
  Track:
    - access_count: How many times object was retrieved
    - last_accessed: When was it last retrieved
    - time_since_ingestion: How old is it

  Categorize:
    - HOT: Accessed in last 7 days, >5 accesses
      └─ Keep in main index, fast retrieval
    - WARM: Accessed in last 30 days, 1-5 accesses
      └─ Keep in secondary index, medium retrieval
    - COLD: Not accessed in 30+ days
      └─ Archive to cheaper storage, slow retrieval

  Smart archival:
    - When storage threshold reached: Move COLD to archive
    - User can still search archive (slower, cheaper)
    - Automatic re-warming: If archived object accessed, move back to main

  Benefit:
    - 80% faster search on active knowledge
    - Reduced storage costs (archive to cheaper tier)
    - Automatic, transparent to user
```

**Benefit:** Operational efficiency; scales to millions of objects without degradation.

**Effort:** High (requires tiered storage architecture)

---

## Related ADRs

- **ADR-037:** Agent-Aware Adaptive Memory (multi-agent coordination)
- **ADR-040:** HEMA (conversation coherence)
- **ADR-013:** Knowledge Graph and Mentions (current entity model)
- **ADR-021:** Multi-Backend Storage (storage layer)
- **ADR-029:** Entity Resolution (conflict detection prerequisite)

---

## What the Paper Gets Right

1. **Four-category framework is useful:** Helps understand design space
2. **Use-case recommendations are sound:** Text-based is correct for our scenario
3. **Acquisition-Management-Retrieval breakdown is actionable:** Identifies component strengths/weaknesses
4. **Explicit memory is the right choice:** For interpretability, auditability, flexibility

---

## What the Paper Misses

1. **Multi-layered hybrid approaches:** Paper treats as orthogonal, but we combine text + vector + graph
2. **Conflict resolution in detail:** Barely mentioned, but critical for long-lived systems
3. **Knowledge evolution tracking:** No guidance on versioning, supersession, contradiction
4. **Operational efficiency:** No discussion of archival, heat-based retrieval, storage tiers

---

## Decision Summary

**What:** Comprehensive taxonomy of memory approaches in LLMs (text-based, KV-cache, parameters, hidden-state).

**Applicability:**
- ✅ **Text-based memory:** Directly applicable (we use this approach)
- ❌ **KV-cache:** Not applicable (we don't run LLM inference)
- ❌ **Parameters:** Not applicable (we don't fine-tune)
- ⚠️ **Hidden-state:** Partially (handled by HEMA ADR-040)

**Recommendation:** **Adopt partially** as internal framework and roadmap validation.

**What we adopt:**
1. Use three-component model (acquisition, management, retrieval) to self-assess
2. Confirm text-based + multi-strategy is correct approach
3. Identify three gaps for Phase 3-4 roadmap

**What we implement:**
- **Phase 3:** Conflict resolution + multi-pass refinement
- **Phase 4:** Heat-based retrieval / cognitive efficiency

**Expected benefits:**
- Better knowledge integrity (detect contradictions)
- Improved search UX (iterative refinement)
- Operational scalability (heat-based archival)

**Cost:** Medium complexity, spread across Phase 3-4

---

## References

- [Cognitive Memory in Large Language Models](https://arxiv.org/abs/2504.02441) (arXiv:2504.02441, Apr 2025)
- Related: ADR-037 (Agent Coordination), ADR-040 (HEMA), ADR-013 (Knowledge Graph)

---

**Status:** This ADR proposes adopting the taxonomy as validation framework and roadmap guidance, with three identified implementation gaps in Phase 3-4.
