# Design Note: Size-Bounded Batch Retrieval

> **Date:** 2026-03-13
> **Status:** Under consideration
> **Source:** Observed in [qmd](https://github.com/tobi/qmd) (`multi-get` with size constraints)
> **Applies to:** dPKMS, ctxt (composition engine, agent integration)

---

## Observation

qmd's `multi-get` operation retrieves a batch of documents by ID or glob pattern, but enforces a `--max-size` constraint (total bytes across all results). When the limit is reached, it stops retrieving further documents. This prevents agent context windows from being silently overflowed when assembling retrieval results.

## Relevance

The composition engine (ADR-017) assembles knowledge objects into briefs, plans, and drafts. Agents invoking the composition engine or the raw search API may request many objects; the total token/byte budget for an LLM context window is finite and known at call time.

Without size bounding, a batch retrieval that returns 20 objects could silently overflow the calling LLM's context window. The agent either gets truncated input (silent quality loss) or an error from the LLM API (hard failure). Neither is acceptable.

## Proposed Pattern

Add `max_bytes` (and optionally `max_tokens`) as first-class parameters on batch retrieval and multi-get operations:

```go
type BatchGetRequest struct {
    IDs      []string
    Glob     string
    MaxBytes int    // 0 = unlimited; stops adding results when cumulative size exceeds limit
    MaxItems int    // 0 = unlimited
}

type BatchGetResponse struct {
    Objects   []KnowledgeObject
    Truncated bool   // true if size/item limit was hit before all requested objects were returned
    TotalSize int    // bytes returned
}
```

When `Truncated=true`, the response includes a `next_cursor` so callers can page through remaining results if needed.

### CLI

```bash
ctxt get --ids id1,id2,id3 --max-bytes 50000
ctxt get --glob "meetings/2026-Q1/**" --max-bytes 100000
```

### Composition Engine Integration

The composition engine (ADR-017) already assembles objects into templates. It should use `max_bytes` derived from the target model's context window minus the template overhead:

```
available_bytes = model_context_limit - template_size - safety_margin
```

This makes the composition engine naturally context-window-aware without requiring callers to calculate budgets manually.

## Trade-offs

- **Size-first vs. rank-first:** When the limit is hit, the current highest-ranked results are kept. Lower-ranked results are dropped. This is the correct behavior — quality over completeness.
- **Bytes vs. tokens:** Bytes are cheaper to compute (no tokenizer needed). Token counting is more accurate but requires a tokenizer per model. Start with bytes; add token counting as an optional provider capability later.
- **Truncation visibility:** `Truncated=true` must be surfaced to callers — silent truncation is the failure mode we are solving.

## Open Questions

- Should `max_tokens` be a separate parameter or derived from `max_bytes` via a ratio?
- Should the composition engine expose a `fit_to_context(model_id)` helper that sets the budget automatically?
- How does this interact with streaming responses (if we add streaming later)?

## Related

- ADR-017 – Composition Engine
- ADR-037 – Agent-Aware Adaptive Memory (agents need reliable context budgeting)
- ADR-061 – Search Quality Tiers (batch retrieval often follows a `best`-tier search)
