# MLPMemory: Relevance Analysis for dPKMS + ctxt

**Date:** 2026-02-18
**Status:** Research phase
**Reference:** [MLP Memory: A Retriever-Pretrained Memory for Large Language Models](https://arxiv.org/abs/2508.01832) (arXiv:2508.01832, Oct 2025)

---

## Executive Summary

**Bottom Line:** MLPMemory is **orthogonal to our current problems**. It optimizes *single-agent inference latency* via parametric memory compression. We're solving *multi-agent coordination latency* via event-driven synchronization.

**Integration Question:** No immediate adoption recommended. Future consideration (Phase 4+) only if we build inference-layer LLM agents.

---

## What MLPMemory Does

### Problem Solved
MLPMemory addresses the RAG vs. Fine-tuning trade-off:
- **RAG:** Flexible knowledge access but slow (retrieval lookup at every inference step)
- **Fine-tuning:** Fast but risks catastrophic forgetting of pretraining knowledge
- **MLPMemory:** Compresses retriever behavior into neural weights; fast + retains knowledge

### Technical Approach
1. Train MLP to mimic k-NN retriever during pretraining
2. MLP learns to predict relevant passages from context (internalizes retrieval patterns)
3. At inference, use interpolated probability between MLP output + Transformer output
4. No explicit document lookup needed → 2.5× faster than RAG

### Performance Results
- **WikiText-103:** 17.5% scaling gain vs. baseline
- **Web datasets:** 24.1% scaling gain
- **QA benchmarks:** 12.3% relative improvement
- **Inference speed:** 2.5× faster than RAG
- **Hallucination reduction:** Up to 10 points on HaluEval

### Key Limitation
Knowledge is **fixed at training time**. Cannot handle:
- Recently published information
- Domain-specific updates (e.g., latest API changes)
- User-specific documents ingested after pretraining

---

## Why MLPMemory Is NOT Relevant to Our Current Roadmap

### 1. **Different Layer, Different Problem**

| Dimension | MLPMemory | Our Problem (AMA/ADR-037) |
|-----------|-----------|-------------------------|
| **Layer** | LLM inference optimization | Agent coordination architecture |
| **Problem** | Single-agent latency (RAG too slow) | Multi-agent synchronization (redundant reasoning) |
| **Solution** | Parametric retrieval (compress kNN into weights) | Event-driven coordination (share findings) |
| **Benefit** | Faster token generation (2.5×) | Faster workflow completion (40-60×) |

### 2. **Architectural Mismatch**

**Our architecture:**
- dPKMS = storage + job queue + knowledge graph (explicit, queryable)
- ctxt = agent coordination + focus profiles + safe execution
- Agents are *deterministic reasoning engines*, not LLMs

**MLPMemory assumes:**
- Large pretrained LLM as the agent
- Knowledge embedded in neural weights
- No explicit knowledge graph or structured reasoning
- Single inference loop (no multi-step orchestration)

**We don't use LLM weights for memory.** Our memory is:
- Entities (knowledge graph)
- Mentions (relationships)
- Reasoning traces (causality)
- All explicit, queryable, updatable

### 3. **Deployment Model Conflict**

**MLPMemory:**
- Requires pretraining on massive corpora (40TB for 5B tokens)
- Knowledge frozen after training
- Can't ingest user documents post-deployment
- Needs periodic retraining for updates

**Our local-first philosophy:**
- Users ingest documents dynamically (today, next week, constantly)
- Knowledge updated in real-time
- No retraining cycle acceptable
- SQLite storage, not neural weights

**Example conflict:** User captures a Slack message today. MLPMemory can't "remember" it without retraining. Our system ingests it immediately.

### 4. **Our Bottleneck Is Not Inference**

**MLPMemory optimizes:** Inference latency (token generation speed)
**Our bottleneck:** Coordination latency (agent synchronization, memory sharing)

Example workflow timing:
```
Current (no coordination):
  Research agent explores → 5 min (finds 10 optimizations)
  Planning agent explores → 5 min (redundantly finds same 10)
  Execution agent runs → 10 min
  Total: 20 min (40% redundancy)

With AMA/ADR-037 coordination:
  Research agent explores → 5 min (publishes findings)
  Planning agent subscribes → 3 min (reads research output, adds planning)
  Execution agent subscribes → 10 min (executes)
  Total: ~8 min (parallelizable parts)
  Speedup: 60%

MLPMemory would optimize:
  Token generation within planning agent from 0.1s to 0.04s
  (Negligible impact on total 20-minute workflow)
```

---

## When MLPMemory *Might* Become Relevant

### Scenario 1: We Build LLM-as-Agent (Phase 4+)

If we decide agents can be LLMs (not just reasoning engines):

```go
// Hypothetical future agent
type LLMAgent struct {
    model    *LargeLanguageModel
    memory   *MLPMemoryModule  // Could integrate here
    context  *FocusProfile
}

func (a *LLMAgent) Reason(task Task) (decision Decision, err error) {
    // LLM generates reasoning with MLPMemory providing fast parametric retrieval
    return a.model.Generate(task, a.memory.RetrievalContext())
}
```

**Preconditions:**
1. We decide agents = fine-tuned LLMs (major architectural shift)
2. We have inference latency problems (not current bottleneck)
3. MLPMemory approach matures (paper is from Oct 2025)

**Current status:** We don't have LLM agents. Agents are deterministic reasoning engines.

### Scenario 2: We Optimize Query Latency (Phase 3)

If semantic search becomes bottleneck (unlikely given current dPKMS):

```
Current: FTS5 + pgvector search in dPKMS
Future (if slow): Could use MLPMemory for faster semantic retrieval

But: This would require:
- Pretraining MLP on our entity/mention graphs
- Accepting knowledge staleness
- Major architectural change
- Doesn't align with local-first philosophy
```

**Probability:** Low. dPKMS already optimized for semantic search.

### Scenario 3: We Build Commercial LLM-Powered Product (Phase 4+)

If ContextHelp becomes an LLM inference service (not just knowledge substrate):

```
E.g., "ContextHelp API" that customers call for:
  POST /api/query
  {"user_id": "...", "question": "..."}
  → Returns answer using MLPMemory for fast inference

Then MLPMemory could reduce per-query latency significantly.
```

**Current status:** We're not building an inference service yet.

---

## Architecture Decision: NOT Adopting MLPMemory (Now)

**Decision:** Do not pursue MLPMemory integration for Phase 1-3.

**Rationale:**
1. **Problem mismatch:** Solves single-agent inference latency; we need multi-agent coordination
2. **Architectural conflict:** Requires pretrained LLM weights; we use explicit knowledge graphs
3. **Deployment incompatibility:** Fixed knowledge at training time; we need real-time ingestion
4. **Wrong bottleneck:** We're not optimizing token generation speed
5. **Phase timing:** Premature; focus on AMA/ADR-037 coordination first

**Future Review Trigger:**
- If we build LLM-as-agent system (Phase 4+)
- If inference latency becomes top-5 performance issue
- If we build commercial API serving inference

---

## Comparative Analysis: What We Actually Need vs. What MLPMemory Provides

| Requirement | dPKMS + ctxt Need | MLPMemory Provides | Gap |
|-------------|------------------|-------------------|-----|
| Multi-agent memory coordination | ✓ Critical | ✗ Single-agent only | **Large** |
| Real-time knowledge ingestion | ✓ Core feature | ✗ Frozen at training time | **Large** |
| Explicit, queryable memory | ✓ Foundation | ✗ Neural weight embeddings | **Large** |
| Local-first operation | ✓ ADR-001 | ✗ Requires pretraining infrastructure | **Large** |
| Causality tracking | ✓ ADR-037 goal | ✗ Black-box neural weights | **Large** |
| Fast parametric retrieval | ✓ Nice-to-have (Phase 4+) | ✓ Provides 2.5× speedup | **Small** |
| Knowledge access during inference | ✓ Secondary use case | ✓ Addresses | **Small** |
| Reduced hallucinations | ✓ Always valuable | ✓ Reduces by ~10 points | **Small** |

**Conclusion:** MLPMemory addresses 2/8 requirements well, but has fundamental conflicts on 6/8.

---

## Related Research: SuperMemory Comparison

Interestingly, **ADR-038** evaluated **SuperMemory** (MIT, cloud-first memory API platform) and rejected it for similar reasons:

**SuperMemory Problem:** Cloud-first agent memory platform
**Our Problem:** Local-first personal knowledge substrate

**MLPMemory Problem:** Single-agent neural memory compression
**Our Problem:** Multi-agent event-driven coordination

Both are orthogonal to our actual architectural needs. The pattern: recent memory research focuses on:
- **Cloud APIs** (SuperMemory)
- **Neural weight compression** (MLPMemory)
- **Agent memory frameworks** (general)

But less work on:
- **Multi-agent coordination** (AMA, now ADR-037)
- **Local-first memory** (implicit in our design)
- **Causality tracking** (rarely featured)

This validates that ADR-037 (based on AMA) is addressing a **genuine gap** in the market.

---

## Recommendation

**Now (Q1 2026):** Focus on ADR-037 (agent coordination via AMA insights)

**Phase 2-3 (Q2-Q4 2026):** Implement AMA foundations (events, profiles, traces)

**Phase 4+ (2027+):** Only reconsider MLPMemory if:
1. We build LLM-based agents (major decision)
2. Inference latency becomes top bottleneck
3. We launch commercial API service
4. MLPMemory matures beyond Oct 2025 state

**File this decision:** Document in decision history for future reference

---

## Sources

- [MLP Memory: A Retriever-Pretrained Memory for Large Language Models](https://arxiv.org/abs/2508.01832) (arXiv:2508.01832, Oct 2025)
- ADR-037: Agent-Aware Adaptive Memory (this project)
- ADR-038: SuperMemory Evaluated and Not Adopted (this project)

