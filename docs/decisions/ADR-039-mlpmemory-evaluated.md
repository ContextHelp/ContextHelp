# ADR-039 – MLPMemory Evaluated and Not Adopted (for Current Phases)

> **Status:** Accepted
> **Date:** 2026-02-18
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt (future consideration)
> **Supersedes:** —
> **Superseded by:** —
>
> **Reference:** [MLP Memory: A Retriever-Pretrained Memory for Large Language Models](https://arxiv.org/abs/2508.01832) (arXiv:2508.01832, Oct 2025)

---

## Context

Recent research (Oct 2025) introduced **MLP Memory**, a parametric memory system that achieves 2.5× faster inference than retrieval-augmented generation (RAG) by compressing retriever behavior into neural network weights. This raised a strategic question: **Should ContextHelp adopt MLPMemory as a core memory mechanism or as an optional component?**

To answer this, we must first understand what problem MLPMemory solves and whether that problem exists in dPKMS + ctxt.

---

## The Problem MLPMemory Solves: RAG Inference Latency

### Traditional RAG Challenge

Standard retrieval-augmented generation requires:

```
LLM inference with RAG:

1. User asks question: "What are the scaling properties?"
2. Retriever searches knowledge base: ~50-200ms (k-NN search)
3. Retriever returns top-k passages
4. LLM reads passages + question
5. LLM generates response token-by-token

Total: Retrieval overhead + generation time
Problem: If generating 100 tokens, retrieval lookup happens every token
         Or if batched: User waits for retrieval before generation starts
```

### The Trade-off MLPMemory Addresses

**Option A: Use RAG (retrieval-augmented)**
- ✅ Flexible (can retrieve from updated knowledge base)
- ✅ Accurate (can access latest information)
- ❌ Slow (50-200ms retrieval latency per query/token)

**Option B: Fine-tune LLM directly**
- ✅ Fast (no retrieval overhead)
- ❌ Catastrophic forgetting (model "forgets" pretraining knowledge)
- ❌ Limited to training data (can't adapt post-deployment)

**Option C: MLPMemory (the innovation)**
- ✅ Fast (2.5× faster than RAG, no explicit retrieval)
- ✅ Retains knowledge (learned retriever patterns during pretraining)
- ⚠️ Knowledge frozen at training time (cannot handle real-time updates)
- ⚠️ Requires large-scale pretraining (substantial compute investment)

### How MLPMemory Works

```
Pretraining Phase:
1. Train MLP to mimic k-NN retriever behavior on massive corpus
2. For each context, MLP learns to predict: "If user asks X, retrieve passages Y"
3. This learning happens via knowledge distillation (matching retriever outputs)
4. Result: MLP encodes retrieval patterns in neural weights (~1B parameters)

Inference Phase:
1. User asks question
2. MLP generates "retrieval context" from weights (no database lookup)
3. Combine MLP output + LLM output via probability interpolation
4. LLM generates response using blended context
5. No explicit retrieval step → 2.5× faster
```

### Performance Results

| Benchmark | Baseline | MLPMemory | Gain |
|-----------|----------|-----------|------|
| WikiText-103 perplexity | 10.42 | 9.58 | 17.5% |
| Web dataset scaling | — | — | 24.1% |
| QA tasks (5 benchmarks) | — | — | 12.3% |
| Inference speed vs RAG | 1.0× | 0.4× | 2.5× faster |
| Hallucination (HaluEval) | Baseline | -10 pts | 10% reduction |

---

## Problem Analysis: Does dPKMS + ctxt Have RAG Latency Issues?

### Our Memory Architecture (Current State)

**dPKMS storage layer:**
```
User data → Ingestion pipeline → dPKMS storage (SQLite/PostgreSQL)
                                  ├─ Entities (knowledge graph)
                                  ├─ Mentions (relationships)
                                  ├─ Attachments (documents, images)
                                  └─ Vectors (embeddings for semantic search)
```

**ctxt retrieval layer:**
```
User query → Query parser (RSQL/NLQ) → Multi-source search
                                        ├─ Local FTS5 (full-text)
                                        ├─ Vector similarity (pgvector/LEANN)
                                        ├─ Remote registries (federated search)
                                        └─ Graph traversal (entity relationships)
            → Reranker (RRF merge) → Results to user
```

### Our Actual Performance Profile

| Operation | Latency | Bottleneck? |
|-----------|---------|------------|
| FTS5 search | ~10-50ms | ❌ No (acceptable) |
| Vector search | ~50-200ms | ❌ No (acceptable for async surfacing) |
| Graph traversal | ~5-20ms | ❌ No |
| Reranking (RRF) | ~5-10ms | ❌ No |
| **Total search latency** | **~100-300ms** | ❌ No (async operation) |
| Knowledge ingestion | ~100-500ms (per object) | ❌ No (backgrounded in job system) |

**Comparison:**
- MLPMemory optimizes: Single LLM token generation latency (milliseconds)
- Our search latency: 100-300ms (acceptable for interactive search)
- We run searches asynchronously (user doesn't wait for results in many flows)

### Why We Don't Have MLPMemory's Problem

#### 1. **We Don't Generate Tokens Over Retrieved Data**

MLPMemory optimizes: `For each token: retrieve context → predict token`

We do: `Search once → return results → user reads at own pace`

Example:
```
MLPMemory scenario (token-by-token generation):
for i in 1..100 {
    context = retriever.Search(query)    // 100ms latency
    token = llm.GenerateToken(i, context) // 1ms latency
    user_sees_token(token)
}
Total: 100 * 100ms = 10 seconds of latency

ContextHelp scenario (search once):
results = search(query)              // 200ms latency (async, user doesn't wait)
composition = compose(results)        // 1-2 seconds (async, user doesn't wait)
user_reads_results_at_leisure()      // No further latency
```

#### 2. **We Handle Dynamic Knowledge via Ingestion Pipeline, Not Re-training**

MLPMemory limitation: Knowledge frozen at training time.

Our solution: Real-time ingestion pipeline (ADR-007: Transactional Outbox).

```
MLPMemory approach to updates:
  Day 1: Train MLP on knowledge corpus (days of compute)
  Day 30: User ingests new document
  Problem: MLP doesn't know about new document (frozen weights)
  Solution: Retrain MLP (days of compute again) ❌ Impractical

ContextHelp approach:
  Day 1: dPKMS ingests user documents via pipeline
  Day 30: User ingests new document
  Solution: Pipeline processes it immediately (seconds)
  Result: New document searchable instantly ✅ Practical
```

#### 3. **We Use Explicit Knowledge Graphs, Not Neural Embeddings**

MLPMemory stores knowledge as: Neural weight patterns (implicit, black-box)

We store knowledge as:
- Entities (structured, queryable)
- Mentions (relationships, explicit)
- Vectors (supplementary, not primary)
- Reasoning traces (causality, auditable)

**Consequence:** We can't use MLPMemory's approach without major architectural change.

```
To use MLPMemory, we would need to:
1. Train MLP on our entire knowledge graph (requires pretraining)
2. Lose ability to query entities directly (replace with neural weights)
3. Lose ability to update knowledge in real-time (frozen after training)
4. Lose ability to track causality (weights are black-box)
5. Loss of local-first operation (need pretraining infrastructure)

This directly violates ADR-001 (local-first), ADR-013 (knowledge graph),
ADR-029 (entity resolution), and ADR-030 (living skeleton).
```

---

## Problem Landscape: Three Different Optimization Goals

To understand why MLPMemory doesn't fit, let's map the three different problems:

### Problem 1: Agent Coordination Latency (Our Focus)

**Problem:** Multi-agent workflows are slow because agents redundantly discover the same information.

**Current state:**
```
Research agent: Explores codebase → 5 min (discovers 10 optimizations)
Planning agent: Independently explores → 5 min (rediscovers same 10)
Execution agent: Waits for plan → 10 min
Total: 20 minutes, 40% redundancy
```

**Solution:** ADR-037 (AMA-inspired event-driven coordination)
```
Research agent: Explores → 5 min (publishes findings via events)
Planning agent: Subscribes to findings → 3 min (no redundant exploration)
Execution agent: Subscribes to plan → 10 min
Total: ~8 minutes (parallelizable), 0% redundancy
Speedup: 60%
```

**Where MLPMemory helps:** Not at all. It doesn't address coordination between agents.

---

### Problem 2: Single-Agent Inference Latency (MLPMemory's Focus)

**Problem:** Generating text from an LLM while retrieving context repeatedly is slow.

**Current state:**
```
for each_token_in_response:
    context = retrieve_from_knowledge_base()  // 100ms
    token = llm.generate(context)              // 1ms
    output_token()
```

**Solution:** MLPMemory (parametric memory compression)
```
mpl_memory = train_mlp_to_mimic_retriever()  // One-time pretraining

for each_token_in_response:
    context = mlp_memory.recall()             // 0.04ms (no lookup)
    token = llm.generate(context)             // 1ms
    output_token()
```

**Speedup:** 2.5× faster (reduces retrieval latency from 100ms to 40ms per token)

**Where MLPMemory helps:** Single-agent LLM text generation loops

---

### Problem 3: Long-Tail Knowledge Updates (Neither Addresses Well)

**Problem:** Knowledge updates require either:
- A. Retraining (MLPMemory, expensive)
- B. Explicit re-ingestion (Our current approach, manageable)

**Trade-off:**
- **MLPMemory:** Fast inference, slow updates
- **ContextHelp:** Fast updates, adequate search latency

We chose the right trade-off for our use case (real-time ingestion matters more than inference speed).

---

## Applicability Analysis: Where MLPMemory Might Fit

### Scenario 1: We Build LLM-as-Agent (Currently: NO)

**Hypothetical future architecture:**

```go
// Today: Deterministic reasoning engine
type Agent interface {
    Reason(context Context, task Task) Decision
}

// Hypothetical future: LLM-based agent
type LLMAgent struct {
    model    *LargeLanguageModel
    memory   *MLPMemoryModule  // Could integrate here
    profile  *FocusProfile
}

func (a *LLMAgent) Reason(ctx Context, task Task) Decision {
    prompt := buildPrompt(task, a.profile)
    // MLPMemory optimizes this:
    reasoning := a.model.GenerateWithMemory(prompt, a.memory)
    decision := parseDecision(reasoning)
    return decision
}
```

**Current status:** We don't build LLM agents. Agents are:
- Deterministic reasoning engines (not neural)
- Focus on coordination, not token generation
- Work with knowledge graphs, not weights

**Probability of future change:** Low-to-Medium (would be Phase 4+ decision)

---

### Scenario 2: We Build Inference API (Currently: NO)

**Hypothetical use case:**

```
Commercial product: "ContextHelp Inference API"

POST /api/generate
{
  "user_id": "123",
  "prompt": "Summarize my architecture decisions",
  "knowledge_scope": "project_x"
}

Response: Generated text using MLPMemory for fast inference
```

**Why MLPMemory would help:**
- Latency-sensitive (customers expect <500ms responses)
- Many inference requests (batching opportunities)
- Knowledge corpus stable per customer (can precompute)

**Current status:** We're not building an inference API

**Probability of future change:** Low (business decision, not technical)

---

### Scenario 3: Search Latency Becomes Bottleneck (Currently: NO)

**Current search performance:**
- FTS5: 10-50ms
- Vector: 50-200ms
- Async operations: User doesn't wait

**When would this become a bottleneck?**
- Interactive search with 1M+ objects (probably not)
- Real-time ranking with 100+ concurrent queries (possible at scale)
- Semantic re-ranking with expensive models (edge case)

**Could MLPMemory help?** Partially, but:
- Would require pretraining on our specific knowledge graphs
- Would lose real-time update capability
- Would add operational complexity (maintaining trained models)
- Better solutions exist: indexing optimization, caching, async precomputation

**Probability this becomes our bottleneck:** Very low

---

## Decision Matrix: When to Reconsider MLPMemory

| Condition | Today | Phase 2-3 | Phase 4+ |
|-----------|-------|----------|---------|
| **Agent type** | Deterministic | Deterministic | LLM? |
| **Bottleneck** | Coordination | Coordination | Inference? |
| **Knowledge update frequency** | Real-time | Real-time | Real-time? |
| **Search latency issue** | No | No | Possibly |
| **Inference API** | No | No | Possibly |
| **MLPMemory fit** | ❌ No | ❌ No | ⚠️ Maybe |

**Trigger for reconsideration:**
1. Build LLM-based agent system AND
2. Inference latency becomes top-5 performance issue AND
3. MLPMemory matures significantly (current: Oct 2025 research)

---

## Architectural Conflicts: Why MLPMemory Can't Be "Optional"

Some technologies can be adopted as optional components (plugins). MLPMemory cannot, due to fundamental conflicts:

### Conflict 1: Knowledge Graph vs. Neural Weights

**Our architecture (ADR-013):**
```go
type Entity struct {
    ID       string
    Namespace string
    Slug     string
    Properties map[string]interface{}
    // Queryable, updatable, explicit
}

type Mention struct {
    EntityID string
    ObjectID string
    Context  string
    // Traceable, explicit relationships
}
```

**MLPMemory approach:**
```
Knowledge encoded in neural weights of MLP
// Black-box, implicit, not queryable
```

**Integration issue:** Can't have both. One replaces the other.

### Conflict 2: Real-Time Ingestion vs. Frozen Training

**Our ingestion (ADR-007):**
```
User captures document → Pipeline processes → dPKMS stores → Immediately searchable
```

**MLPMemory approach:**
```
Pretraining dataset → Train MLP → Freeze weights → Deploy
New documents → ??? (not handled without retraining)
```

**Integration issue:** MLPMemory's knowledge is frozen after deployment. Our system's value is real-time ingestion.

### Conflict 3: Deterministic Execution vs. Neural Inference

**Our approach:**
- Deterministic reasoning (for safety, auditability, reproducibility)
- Explicit logic (understandable, debuggable)
- No non-determinism

**MLPMemory approach:**
- Neural inference (probabilistic, stochastic)
- Black-box embeddings (opaque)
- Non-deterministic by nature

**Integration issue:** Adding neural components would violate our determinism guarantees.

### Conflict 4: Local-First vs. Pretraining Infrastructure

**Our philosophy (ADR-001):**
- No external dependencies
- Works offline
- SQLite default (zero-install)

**MLPMemory requirement:**
- Large pretraining corpora (40TB+ data)
- Significant compute (days of GPU time)
- Cannot be computed locally without massive infrastructure

**Integration issue:** MLPMemory violates local-first philosophy.

---

## What We Can Learn From MLPMemory (Without Adopting It)

Even though we're not adopting MLPMemory, the research offers insights:

### Insight 1: Parametric Knowledge Has Trade-Offs

**MLPMemory showed:**
- ✅ Parametric memory can be faster than retrieval
- ❌ But at cost of staleness and reduced flexibility

**Lesson for us:** Our explicit knowledge graph trade-off (slower search, real-time updates, queryable) is correct for our use case. Not better or worse—just different.

### Insight 2: Memory Compression is Valuable in Specific Contexts

**MLPMemory optimizes:** Token-level retrieval in inference loops

**Equivalent optimization for us:** Could compress entity graphs for edge deployment

Potential future work:
```
For offline/edge deployment:
- Snapshot entity graph at deployment time
- Compress for bandwidth (50-100MB)
- Store on mobile/edge device
- Enable offline search (no network)

This is similar spirit to MLPMemory but applied to our knowledge graphs.
```

### Insight 3: Pre-computation > On-Demand Retrieval (When Possible)

**MLPMemory insight:** Pre-compute retrieval patterns (during training) beats on-demand lookup.

**Our equivalent:** Pre-compute rankings, summaries, relationships where appropriate.

Potential future application:
```
Phase 3 optimization: Precompute focus profiles
- For each focus profile, precompute "top entities for this profile"
- Precompute "summary of this entity in context of this profile"
- Cache relationships frequently accessed together
```

### Insight 4: Frozen Knowledge Requires Good Versioning

**MLPMemory limitation:** Knowledge frozen after training

**Lesson for us:** If we ever pre-compute anything, need excellent versioning and rollback.

---

## Related ADRs

- **ADR-001:** Local-First and Decentralized (conflicts with MLPMemory's pretraining requirements)
- **ADR-007:** Transactional Outbox (real-time ingestion; MLPMemory incompatible)
- **ADR-013:** Knowledge Graph and Mentions (explicit memory; MLPMemory is implicit)
- **ADR-029:** Entity Resolution (queryable entities; MLPMemory is black-box)
- **ADR-037:** Agent-Aware Adaptive Memory (coordination focus; not inference focus)

---

## Implementation Notes

### If We Reconsider MLPMemory in Phase 4+

Should reconsideration happen, evaluation criteria:

1. **Agent architecture decision** – Are agents LLM-based? (Currently: no)
2. **Performance profiling** – Is inference latency in top-5 issues? (Currently: no)
3. **Operational commitment** – Can we maintain pretraining infrastructure? (Currently: no)
4. **Knowledge staleness tolerance** – Can users accept frozen-at-training knowledge? (Currently: no)
5. **MLPMemory maturity** – Is it production-ready? (Currently: Oct 2025 research)

Only proceed if all 5 are "yes".

### What Would Change in Architecture

If we did adopt MLPMemory:

```
Phase 4+ hypothetical:

1. Create MLPMemory layer for inference-focused agents
2. Keep existing dPKMS for storage/coordination
3. Build sync mechanism: dPKMS entities → pretraining dataset
4. Establish retraining cadence (quarterly? annually?)
5. Maintain dual paths: real-time ingestion + periodic MLPMemory updates
6. Accept trade-off: some users see stale knowledge, some see real-time

This would make system more complex, not simpler.
Most likely to pursue only if:
- Inference latency becomes acute problem (it won't)
- Customer demand is high (it isn't)
- ROI clearly positive (it isn't)
```

---

## Decision Summary

**What:** Evaluated MLPMemory (parametric memory system with 2.5× faster inference)

**Why we looked:** Could it accelerate our agent memory coordination?

**Conclusion:** Not applicable to current architecture or roadmap.

**Key differences:**
| Dimension | MLPMemory Problem | Our Problem |
|-----------|-----------------|------------|
| **Bottleneck** | Single-agent inference latency | Multi-agent coordination latency |
| **Solution domain** | Neural weight compression | Event-driven coordination |
| **Knowledge model** | Frozen at training time | Updated in real-time |
| **Storage** | Neural weights (implicit) | Knowledge graphs (explicit) |
| **Execution model** | Inference (probabilistic) | Reasoning (deterministic) |
| **Deployment model** | Requires pretraining infra | Local-first, zero-install |

**Decision:** Not adopting MLPMemory.

**Reconsideration trigger:** Phase 4+ only if:
1. We build LLM-based agents
2. Inference latency becomes top-5 issue
3. We're willing to sacrifice real-time ingestion for speed

**Current probability of change:** <5%

**File location of analysis:** `docs/mlpmemory-relevance-analysis.md`

---

## References

- [MLP Memory: A Retriever-Pretrained Memory for Large Language Models](https://arxiv.org/abs/2508.01832) (arXiv:2508.01832, Oct 2025)
- GitHub: [Rubin-Wei/MLPMemory](https://github.com/Rubin-Wei/MLPMemory)
- Related: ADR-037 (Agent-Aware Adaptive Memory)
- Related: ADR-038 (SuperMemory Evaluated)

---

**Status:** This ADR documents our evaluation and rejection of MLPMemory for current phases. It remains a reference point for future architectural decisions in Phase 4+.
