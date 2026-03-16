# US-0021: Search with Result Explanation

**System Types:** ctxt, dpkms (self-hosted), dpkms cloud
**Personas:** [Knowledge Workers](../../personas/knowledge-workers.md), [Agents/LLMs](../../personas/agents-llms-tools.md)

---

## User Goal

As a user, I want each search result to include an explanation of why it matched so that I can trust and understand the ranking rather than treating it as a black box.

---

## Context

Relevance ranking is invisible by default. A result at position 3 may be there because it had a strong vector match, or a graph connection, or a profile boost — but without explanation, the user cannot tell. Adding `--explain` surfaces the per-strategy score contributions in a `rank.explain` object on every result, including `graph_match`, `vector_similarity`, `fts_score`, `mention_boost`, and `profile_boost` fields, plus a human-readable `primary_match_reason` string. Explanations are generated server-side only when requested, keeping default response sizes small.

---

## Acceptance Criteria

- [ ] User can request explanations: `ctxt find "query" --explain`
- [ ] Without `--explain`, `rank.explain` is absent from results; `rank.score` is still present
- [ ] With `--explain`, every result includes a `rank.explain` object with per-strategy score contributions
- [ ] `rank.explain.primary_match_reason` is a non-empty string describing why the result matched
- [ ] `rank.score` is a float in [0, 1]; per-strategy scores in `rank.explain` are consistent with the overall score
- [ ] Graph-matched results have a non-zero `rank.explain.graph_match` score
- [ ] Vector-matched results have a non-zero `rank.explain.vector_similarity` score
- [ ] FTS-matched results have a non-zero `rank.explain.fts_score`
- [ ] If explanation generation fails, the result is returned with `rank.score` intact and `rank.explain` set to null with an error note (not 500)
- [ ] Same query with `--explain` via CLI, REST (`?explain=true`), and gRPC returns identical explanations and scores

---

## Implementation Notes

[Detailed implementation guide to be filled in]

---

## E2E Test Checklist

### CLI → Server Payload

- [ ] `ctxt find "query" --explain` sends `explain=true` in request payload; server receives
      the field and includes `rank.explain` in each result
- [ ] `ctxt find "query"` without `--explain` sends `explain=false` (or absent); server omits
      `rank.explain` from results (lower response size)
- [ ] `ctxt find "query" --explain --limit 5` sends both `explain` and `limit` in payload;
      server receives both

### Server-Side Receipt and Storage

- [ ] Server receives `explain=true`; each result in response includes `rank.explain` object
- [ ] `rank.explain` populated server-side with per-strategy score contributions
      (e.g., `graph_match`, `vector_similarity`, `fts_score`, `mention_boost`)
- [ ] `rank.explain.primary_match_reason` is a non-empty string describing why result matched
- [ ] `rank.score` is a float in [0, 1]; per-strategy scores in `rank.explain` sum consistently
      with overall score
- [ ] Without `explain=true`, `rank.explain` is absent or null in response (not populated)

### Flags Coverage

- [ ] `--explain` — `explain=true` present in payload; `rank.explain` present in every result
- [ ] `--limit <n>` — present in payload; result count ≤ n
- [ ] No `--explain` flag → `rank.explain` absent from response; overall `rank.score` still
      present

### Explanation Correctness

- [ ] Graph-matched result has non-zero `rank.explain.graph_match` score
- [ ] Vector-matched result has non-zero `rank.explain.vector_similarity` score
- [ ] FTS-matched result has non-zero `rank.explain.fts_score`
- [ ] Multi-strategy result has multiple non-zero strategy scores in `rank.explain`
- [ ] `primary_match_reason` accurately identifies the dominant matching strategy for each result

### Error Handling

- [ ] Explanation generation failure (AI provider error) → result returned with `rank.score`
      intact; `rank.explain` set to null with error note, not 500

### Interface Parity

- [ ] Same query with `--explain` via CLI, REST (`?explain=true`), and gRPC → identical
      explanations and scores per result

---

## Related Stories

- [US-0016](./US-0016-natural-language-search.md) — Natural Language Search (`rank.explain` included in NLQ results)
- [US-0018](./US-0018-multi-strategy-search-execution.md) — Multi-Strategy Search Execution (per-strategy scores populate `rank.explain`)
- [US-0020](./US-0020-apply-focus-profile-to-search.md) — Apply Focus Profile to Search (`profile_boost` is one explanation field)
- [US-0061](./US-0061-visual-similarity-search.md) — Visual Similarity Search (`match_sources` annotations complement `rank.explain`)
