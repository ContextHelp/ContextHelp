# ADR-044 – ContextualRetriever: Context-Aware Retrieval for Conversational Search

> **Status:** Proposed
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** —
> **Superseded by:** —
>
> **Reference:** [Learning Contextual Retrieval for Robust Conversational Search](https://arxiv.org/abs/2509.19700) (arXiv:2509.19700, Sep 2025, EMNLP 2025 main conference, Seunghan Yang et al.)

---

## Context

Recent research (EMNLP 2025) introduced **ContextualRetriever**, a retrieval mechanism specifically designed for multi-turn conversational search. The question: **Should we adopt this approach for our iterative search refinement scenarios?**

This ADR evaluates whether ContextualRetriever addresses a genuine gap in dPKMS + ctxt and whether adoption is justified.

---

## The Problem ContextualRetriever Solves

### Multi-Turn Conversational Search Challenge

In conversational search, users interact through multiple turns with evolving context:

```
Turn 1: "Show me architecture decisions"
  → System retrieves: [D1, D2, D3]

Turn 2: "Filter to high-impact ones"
  → User expects: Filter previous results
  → Current approach: Requires re-specifying full query

Turn 3: "From those, which ones affect the API?"
  → User expects: System understands we're still in "architecture decisions" context
  → Problem: Without context, system might search entire knowledge base for "API"
  → Missed: The API questions were specific to architecture decisions discussed earlier

Turn 4: "Remind me what the first one was"
  → User expects: System remembers "first" = D1 from Turn 1
  → Problem: Without conversation history, "first" is ambiguous
```

### Why Query Rewriting Isn't Sufficient

**Current approach (query rewriting):**
```
Turn 2: User says "Filter to high-impact ones"
System rewrites: "High-impact architecture decisions"
Result: More accurate retrieval ✓

But:
- Requires extra inference step (autoregressive generation)
- Adds 100-500ms latency per turn
- Can accumulate errors (rewritten query might be wrong)
- Expensive at scale (extra LLM calls)
```

### ContextualRetriever's Innovation

**New approach (context-aware embedding):**
```
Turn 2: User says "Filter to high-impact ones"
System:
  1. Encodes CONTEXT: Full conversation history
  2. Highlights CURRENT: The new query "high-impact"
  3. Embeds: Special attention to current query within history
  4. Retrieves: Directly from embeddings (no extra inference)
  5. Filters: Results by context understanding

Result:
- No extra inference step
- Latency: Same as single query (~50-100ms)
- Accuracy: Better than rewriting (learns from conversation)
- Cost: Same as baseline retrieval
```

---

## Technical Approach: How ContextualRetriever Works

### Three Core Components

#### 1. Context-Aware Embedding Mechanism

**What it does:** Processes entire conversation history while highlighting the current query.

```
Input:
  History: ["Show me architecture decisions", "Filter to high-impact"]
  Current: "From those, which ones affect the API?"

Processing:
  1. Embed history tokens (all prior turns)
  2. Mark current query with special tokens/attention
  3. Combine: history embeddings + current query attention
  4. Output: Single embedding that represents "API questions within architecture decisions context"

Benefit:
  - Embedding captures conversational context implicitly
  - Current query is NOT just "API" but "API within architecture decisions"
  - No explicit query rewriting needed
```

#### 2. Intent-Guided Supervision (IGL)

**What it does:** Train the retriever using high-quality rewritten queries as supervision.

```
Training:
  1. For each conversation turn:
     - Get user query: "high-impact ones"
     - Get rewritten query (from oracle): "High-impact architecture decisions"
     - Get relevant passages: [D1, D3] (high-impact decisions)

  2. Train retriever to match:
     - context_embedding(history + "high-impact ones")
     - ≈ rewritten_query_embedding("High-impact architecture decisions")
     - ≈ passage_embedding(D1, D3)

  3. Result: Retriever learns to understand implicit context without explicit rewriting
```

#### 3. Conversational Contrastive Learning (CCL)

**What it does:** Learns to distinguish relevant vs. irrelevant passages in conversational context.

```
Positive pairs:
  - Context embedding + Relevant passage in this conversation turn

Negative pairs:
  - Context embedding + Irrelevant passage
  - Context embedding + Passage from different conversation
  - Context embedding + Passage from earlier turns (now irrelevant due to topic shift)

Result:
  - Learns what "relevant given this conversation" means
  - Handles topic drift (when user switches topics)
  - Handles Historical Interference (passages relevant to old topics become irrelevant)
```

---

## Performance Results

### Benchmarks Evaluated

**Datasets:**
- TREC-CAsT (Conversational search track)
- TopiOCQA (Topic-Oriented Conversational QA)
- QReCC (Question Rewriting and Contextualization)

### Key Metrics

| Metric | Meaning | Result |
|--------|---------|--------|
| **Hit@100** | % of queries where relevant passage in top-100 results | ✅ Significantly improved |
| **HIR@100** | Historical Interference Rate (false positives from old context) | ✅ Reduced |
| **Latency** | Time to retrieve results | ✅ Same as baseline (no overhead) |
| **Failed retrievals** | Queries with no relevant results | ✅ Reduced by 49% |
| **Failed retrievals + reranking** | Combined with reranking step | ✅ Reduced by 67% |

### Comparison to Baselines

**vs. Query Rewriting:**
- ✅ Same accuracy (uses rewritten queries for training)
- ✅ Better latency (no inference overhead)
- ✅ More efficient (no extra LLM calls)

**vs. Standard Dense Retrieval (DPR):**
- ✅ Better accuracy on conversational queries
- ✅ Handles context and topic drift
- ✅ No additional parameters (same model size)

**vs. LLM-Based Retrievers (e.g., GritLM):**
- ✅ Competitive or better accuracy
- ✅ Explicit conversation awareness
- ✅ More efficient training

---

## Applicability to dPKMS + ctxt

### Where It Fits

**Current Architecture:**
```
ctxt (Knowledge Workers / Iterative Search):
├─ User enters: "Show me architecture decisions"
├─ Multi-strategy search executes:
│  ├─ FTS (full-text search)
│  ├─ Vector search (semantic similarity)
│  ├─ Graph traversal (entity relationships)
│  ├─ Metadata filtering (SQL)
│  └─ Reranking (RRF merge)
└─ Results returned

Turn 2: User enters "Filter to high-impact"
├─ Current: Re-query entire knowledge base with "high-impact" + "architecture"
├─ Problem: Loses refinement context
└─ With ContextualRetriever: Adds conversation context to each strategy
```

**What ContextualRetriever Would Add:**
```
Enhanced Search Pipeline:
├─ User conversation history maintained
├─ Context encoding:
│  ├─ Encode conversation so far
│  ├─ Highlight current query in context
│  └─ Create context-aware embedding
├─ Multi-strategy search (same as before):
│  ├─ FTS (enhanced with context bias)
│  ├─ Vector search (using context-aware embedding)
│  ├─ Graph traversal (same)
│  └─ Metadata (same)
├─ Reranking (RRF, possibly context-aware)
└─ Results returned with better accuracy
```

### Three Application Scenarios

#### Scenario 1: Iterative Search Refinement (High Fit)

**Current:**
```
Turn 1: "Show me decisions"
  Query: type==decision
  Results: [D1, D2, D3, D4]

Turn 2: "Only engineering ones"
  Query: type==decision AND team==engineering
  Results: [D1, D3]
  Issue: User must re-specify "decision" constraint

Turn 3: "Ordered by recency"
  Query: type==decision AND team==engineering ORDER BY created_at DESC
  Issue: Full re-query each time
```

**With ContextualRetriever:**
```
Turn 1-2: Same (first two turns work fine)

Turn 3: "Ordered by recency"
  System understands: "recency" applies to "engineering decisions"
  Doesn't need: Full re-query with all prior constraints
  Result: Faster, more accurate refinement
```

**Fit:** ⭐⭐⭐⭐⭐ Excellent (directly addresses iterative search)

---

#### Scenario 2: Knowledge Worker Multi-Turn Composition (High Fit)

**Use case:** User building a brief through iterative questions

```
Turn 1: "What were the key decisions about mobile?"
Turn 2: "Of those, which are still open?"
Turn 3: "Who's responsible for each?"
Turn 4: "What's the timeline?"
Turn 5: "Show me the rationale for the database decision specifically"

Without context:
  Turn 5: System searches entire knowledge base for "database decision rationale"
  Result: May find unrelated decisions with "database" mention

With ContextualRetriever:
  Turn 5: System understands "database decision" = specific one discussed in Turn 1
  Result: Correct decision found immediately
```

**Fit:** ⭐⭐⭐⭐ Very good (multi-turn queries benefit from context)

---

#### Scenario 3: Agent Multi-Turn Reasoning (Medium Fit)

**Use case:** Agents asking follow-up questions during analysis

```
Agent Turn 1: "Analyze payment system architecture"
Agent Turn 2: "What are the bottlenecks?"
Agent Turn 3: "How does this compare to the order system?"

Without context:
  Turn 3: "order system" could match any order-related content
  Ambiguous: Does agent mean order processing architecture?

With ContextualRetriever:
  Turn 3: System knows "order system" = architecture system for comparison
  Result: Correct comparison made
```

**Fit:** ⭐⭐⭐ Good (helps agent queries, but not as critical as human search)

---

## Gap Analysis: What ADR-042 Identified vs. What ContextualRetriever Provides

**ADR-042 Gap #2: Multi-Pass Refinement**

Gap description:
> "Problem: Iterative search requires full re-query each turn"

ContextualRetriever solution:
> "Uses context-aware embeddings to understand implicit constraints"

**Relationship:**
- ContextualRetriever is a **technical implementation** of multi-pass refinement
- ADR-042 identified the problem; ContextualRetriever provides the solution
- They're complementary: ADR-042 is "what", ContextualRetriever is "how"

**Difference in approach:**
```
ADR-042 proposal: Cache results from pass 1, filter in pass 2
  Pass 1: find architecture decisions [D1, D2, D3, D4]
  Cache: save results
  Pass 2: filter([D1, D2, D3, D4], impact==HIGH) → [D1, D3]

ContextualRetriever approach: Context-aware embedding
  Turn 1: embed("Show architecture decisions", history=[])
  Turn 2: embed("high-impact", history=["Show architecture decisions"])
  Special: Turn 2 embedding implicitly knows it's filtering Turn 1 results
```

**Which is better?**
- **ADR-042 approach:** Simpler, explicit, doesn't require training
- **ContextualRetriever:** More elegant, learned behavior, better for ambiguous queries

**Recommendation:** Implement both:
1. Use caching for efficiency (ADR-042)
2. Use context-aware retrieval for accuracy (ContextualRetriever)

---

## Technical Integration with dPKMS

### Required Changes

**1. Conversation State Management**
```go
type ConversationState struct {
    ID            string
    UserID        string
    Turns         []SearchTurn
    EmbeddingHistory []embedding.Vector  // Store embeddings of each turn
}

type SearchTurn struct {
    TurnNumber   int
    UserQuery    string
    Results      []ObjectID
    Context      Context  // Conversation context at this turn
}
```

**2. Enhanced Vector Search**
```go
// Current:
func (s *Search) SemanticSearch(query string) []Result

// Enhanced:
func (s *Search) ContextualSemanticSearch(
    query string,
    conversationID string,
    turnNumber int,
) []Result {
    // 1. Retrieve conversation history
    history := getConversationHistory(conversationID, turnNumber)

    // 2. Create context-aware embedding
    contextEmbedding := createContextEmbedding(query, history)

    // 3. Search using context embedding
    return semanticSearch(contextEmbedding)
}
```

**3. Training Pipeline**
```
Data source: Captured conversations + ground truth (relevant passages)
Training:
  - For each conversation + turn
  - Create context-aware embeddings
  - Train to match: relevant passages + rewritten query embeddings
  - Validate on held-out conversations
```

### No Breaking Changes Required
- Existing search still works
- Context-aware search is opt-in via new parameter
- Gradual rollout possible

---

## Implementation Approach

### Phase Recommendation: Phase 3 (Q3-Q4 2026+)

**Why Phase 3:**
- Depends on ADR-040 (HEMA) - conversation storage infrastructure
- Depends on ADR-042 (conflict resolution + multi-pass) - refinement foundation
- Requires training data (need enough user conversations)
- Medium complexity, high value-add

### Implementation Steps

**Step 1: Foundation (Week 1-2)**
- Store conversation history (build on HEMA infrastructure)
- Create embedding storage for conversation turns
- Implement context-aware embedding generation

**Step 2: Model Training (Week 3-6)**
- Collect training data from conversational queries
- Implement CCL + IGL training pipeline
- Fine-tune base embedding model (or use pretrained if available)
- Validate on TREC-CAsT, TopiOCQA, QReCC benchmarks

**Step 3: Integration (Week 7-8)**
- Add contextual retrieval as option in multi-strategy search
- Wire into conversation manager (from HEMA)
- A/B test: context-aware vs. standard retrieval

**Step 4: Optimization (Week 9-12)**
- Profile latency (should be same as baseline)
- Tune model for dPKMS-specific knowledge domains
- Document integration patterns

### Success Criteria
- ✅ Failed retrievals reduced by 40%+ on conversational queries
- ✅ No latency regression (< 5% vs. baseline)
- ✅ Works with existing multi-strategy ranking
- ✅ Graceful degradation if conversation history unavailable

---

## Tradeoffs & Risks

### Tradeoff 1: Training vs. Generalization

**Cost:** Need to train/fine-tune model on conversational data

**Benefit:** Better accuracy than generic models

**Mitigation:**
- Start with pretrained contextual models (if available)
- Use transfer learning from public conversational datasets
- Only fine-tune final layer on domain-specific data

**Risk level:** Medium (manageable with good training data)

---

### Tradeoff 2: Model Staleness

**Cost:** Embedding model is learned; new conversation patterns not reflected until retraining

**Example:** If conversation patterns change (new topics, new terminology), model becomes outdated

**Mitigation:**
- Periodic retraining (quarterly or biannually)
- Monitor retrieval failures as early warning
- Implement online learning (minor parameter updates per turn)

**Risk level:** Low (periodic retraining is standard)

---

### Tradeoff 3: Complexity vs. Benefit

**Cost:** More complex than simple filtering/caching (ADR-042)

**Benefit:** Better handling of ambiguous/implicit queries

**When to choose ADR-042 only:** If users always re-specify full context
**When to add ContextualRetriever:** If users frequently use implicit references ("that one", "from before", etc.)

**Risk level:** Low (can be added incrementally)

---

### Tradeoff 4: Training Data Quality

**Problem:** Model trained on rewritten queries (from oracles or LLMs) may perpetuate errors

**Example:** If oracle rewrites are wrong, model learns wrong patterns

**Mitigation:**
- Use high-quality rewrites (human-verified or from strong models)
- Implement quality checks in training loop
- Have explicit fallback to standard retrieval if confidence low

**Risk level:** Medium (data quality critical)

---

## Comparison: ContextualRetriever vs. Related Work

| Aspect | ADR-042 (Caching) | ContextualRetriever | HEMA (ADR-040) |
|--------|---|---|---|
| **Problem solved** | Multi-pass efficiency | Implicit context understanding | Long-context narrative coherence |
| **Mechanism** | Result caching + filtering | Context-aware embedding | Compact memory + vector retrieval |
| **Latency** | Faster (no requery) | Same as baseline | Additional for memory updates |
| **Accuracy** | Good (explicit) | Excellent (learned) | Excellent (narrative) |
| **Complexity** | Low | Medium | Medium |
| **Training needed** | No | Yes | Optional (inference-only possible) |
| **Use case** | Explicit refinement | Implicit queries | Long conversations |
| **Integration** | Immediate (Phase 3) | Phase 3 | Phase 3 |

---

## Decision: Adopt ContextualRetriever for Phase 3

### What We Adopt

**Core approach:** Context-aware embeddings for conversational search retrieval

**Training strategy:**
1. Start with transfer learning from public datasets (TREC-CAsT, etc.)
2. Fine-tune on domain-specific conversational queries
3. Periodic retraining (quarterly) as conversation patterns evolve

**Integration point:** Multi-strategy search pipeline (enhance vector search component)

### Why We Adopt

1. **High relevance:** Directly solves ADR-042 Gap #2 (multi-pass refinement)
2. **Proven results:** 49-67% reduction in failed retrievals (published benchmarks)
3. **No latency cost:** Same as baseline retrieval (no extra inference)
4. **Incremental:** Can be added without breaking existing search
5. **Complements existing:** Works well with multi-strategy ranking

### How We Integrate

**Short term (Phase 3):**
- Add context-aware search as option (opt-in)
- Run parallel with standard search (A/B testing)
- Monitor accuracy improvement

**Medium term (Phase 4+):**
- Make context-aware default for conversational queries
- Tune model on domain data
- Integrate with agent reasoning (scenario 3)

### What We Don't Adopt

- ❌ Query rewriting (ContextualRetriever replaces this)
- ❌ Fine-tuning entire model (only embedding layer)
- ❌ Online learning (stick to periodic retraining)

---

## Related ADRs

- **ADR-040:** HEMA (conversation storage infrastructure)
- **ADR-042:** Taxonomy gaps (multi-pass refinement is Gap #2)
- **ADR-037:** Agent coordination (agents benefit from better search)
- **ADR-013:** Knowledge graph (retrieved results still use graph)
- **ADR-021:** Storage (need to store embeddings)

---

## Implementation Notes

### Dependencies
- Conversation state management (from HEMA ADR-040)
- Result caching infrastructure (from ADR-042)
- Vector storage with update capability

### Data Requirements
- Training: ~10K conversations with relevance labels
- Evaluation: ~1K conversational queries with ground truth
- Estimate: Available from TREC-CAsT + internal users (Phase 2-3)

### Model Size
- Embedding model: ~500M-1B parameters
- Inference: Fast (single forward pass, no generation)
- Serving: Same as current vector search

---

## Decision Summary

**What:** ContextualRetriever - LLM-based retrieval that incorporates conversation history through context-aware embeddings

**Problem solved:** Multi-turn conversational search loses context; implicit user references become ambiguous

**Key innovation:** Learns to understand user intent from conversation without explicit query rewriting

**Performance:** 49-67% reduction in failed retrievals, same latency as baseline

**Decision:** **ADOPT** for Phase 3 (Q3-Q4 2026+)

**Benefits:**
- Better UX for iterative search (users can use implicit references)
- Handles topic drift and historical interference
- No latency overhead
- Integrates cleanly with existing multi-strategy search

**Implementation:** Training + fine-tuning in Phase 3, integration with ADR-040 (HEMA) and ADR-042 (multi-pass refinement)

**Expected impact:** Directly addresses ADR-042 Gap #2; enables enterprise conversational search scenarios

---

## References

- [Learning Contextual Retrieval for Robust Conversational Search](https://arxiv.org/abs/2509.19700) (arXiv:2509.19700, Sep 2025, EMNLP 2025)
- [ACL Anthology Paper](https://aclanthology.org/2025.emnlp-main.602.pdf)
- Related: ADR-037 (Agent Coordination), ADR-040 (HEMA), ADR-042 (Memory Taxonomy)

---

**Status:** This ADR proposes adopting ContextualRetriever as part of Phase 3 search optimization, specifically addressing ADR-042 Gap #2 (multi-pass query refinement) for conversational search scenarios.
