# US-0018: Multi-Strategy Search Execution

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Agents/LLMs](../../personas/agents-llms-tools.md), [Knowledge Workers](../../personas/knowledge-workers.md)

---

## User Goal

As a user, I want the search system to execute multiple strategies (graph, vector, FTS, metadata) in parallel and merge their results so that I get higher-quality, more comprehensive results than any single strategy alone.

---

## Context

No single search strategy covers all cases: keyword search misses semantic equivalents; vector search misses exact terms; graph search finds related entities but not text matches. Multi-strategy execution runs all applicable strategies in parallel, then merges results using Reciprocal Rank Fusion (RRF). An object found by multiple strategies receives a boosted rank. Users can explicitly specify strategies or let the system auto-select based on query intent. The response always reports which strategies were actually executed so results can be understood and reproduced.

---

## Acceptance Criteria

- [ ] User can specify strategies explicitly: `ctxt find "query" --strategy graph,vector`
- [ ] Without `--strategy`, server auto-selects strategies based on query intent
- [ ] Server executes listed strategies in parallel, not sequentially
- [ ] Results from multiple strategies are merged via RRF; objects matched by multiple strategies rank higher
- [ ] Response includes `strategies_used` listing all strategies actually executed
- [ ] Each result includes `rank.explain` with per-strategy score contributions
- [ ] Duplicate objects (same ID from multiple strategies) are collapsed to a single result with merged score
- [ ] If one strategy backend is unavailable, remaining strategies still execute and response notes the skipped strategy
- [ ] Invalid strategy name returns 400 with an unknown-strategy error
- [ ] Same query + strategies via CLI, REST, and gRPC returns identical result sets and merge scores

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --strategy graph,vector` sends `strategies=["graph","vector"]` in request
      payload; server receives both strategy names
- [ ] `ctxt find "query" --strategy fts` sends `strategies=["fts"]`; server executes only FTS
- [ ] `ctxt find "query"` (no strategy flag) sends `strategies` absent or `"auto"`; server
      selects strategies automatically
- [ ] `ctxt find "query" --limit 10` sends `limit=10` in request payload; server receives it

### Server-Side Receipt and Storage

- [ ] Server receives `strategies` field and executes only the listed strategies; response
      `strategies_used` matches request `strategies`
- [ ] Server executes listed strategies in parallel (not sequentially); `query_time_ms` ≤ max
      single-strategy time + overhead
- [ ] Server merges multi-strategy results via RRF; response results include `rank.explain`
      with per-strategy scores
- [ ] Server receives `limit`; result count ≤ limit
- [ ] Server response includes `strategies_used` listing all strategies actually executed

### Flags Coverage

- [ ] `--strategy <list>` — strategy names present in payload; server executes and reports only
      those strategies
- [ ] `--limit <n>` — present in payload; result count ≤ n
- [ ] Invalid strategy name → server returns 400 with unknown-strategy error

### Strategy Execution

- [ ] Graph strategy: results include objects connected via entity relationships
- [ ] Vector strategy: results include semantically similar objects (cosine similarity)
- [ ] FTS strategy: results include objects with keyword matches
- [ ] Metadata strategy: results filtered by structured fields (type, tags, date)
- [ ] Multi-strategy: same object found by multiple strategies receives boosted rank vs.
      single-strategy match

### Merge and Ranking

- [ ] RRF merge applied when multiple strategies active; response shows per-strategy rank
      contributions in `rank.explain`
- [ ] Duplicate objects from multiple strategies collapsed to single result with merged score
- [ ] Results ordered by final merged score descending

### Error Handling

- [ ] One strategy backend unavailable → remaining strategies still execute; response notes
      which strategy was skipped
- [ ] All strategies return empty → `{"results": [], "total": 0}` with 200 status

### Interface Parity

- [ ] Same query + strategies via CLI, REST, gRPC → identical result sets and merge scores

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (NLQ drives auto strategy selection)
- [US-0017](./US-0017-structured-rsql-query.md) — Structured RSQL Query (metadata strategy; single-strategy deterministic path)
- [US-0021](./US-0021-search-with-result-explanation.md) — Search with Result Explanation (`rank.explain` populated here)
- [US-0051](./US-0051-semantic-search-with-embeddings.md) — Semantic Search with Embeddings (vector strategy detail)
- [US-0052](./US-0052-graph-based-entity-search.md) — Graph-Based Entity Search (graph strategy detail)
- [US-0061](./US-0061-visual-similarity-search.md) — Visual Similarity Search (visual strategy added to pipeline)

---

## Personas

- [Knowledge Workers](../../personas/knowledge-workers.md)
- [Researchers / OSINT](../../personas/researchers-osint.md)
- [Agents / LLMs / Tools](../../personas/agents-llms-tools.md)

---

## E2E Tests

- `test/integration/us0018_multi_strategy_test.go::TestUS0018_FTSAndRSQLPathsBothReturn`
- `test/integration/us0018_multi_strategy_test.go::TestUS0018_HybridSearchDeduplicate`
- `test/integration/us0018_multi_strategy_test.go::TestUS0018_ExplainContainsScoreBreakdown`
