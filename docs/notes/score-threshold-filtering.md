# Design Note: Score Threshold Filtering

> **Date:** 2026-03-13
> **Status:** Under consideration
> **Source:** Observed in [qmd](https://github.com/tobi/qmd) (`--min-score`)
> **Applies to:** dPKMS, ctxt

---

## Observation

qmd exposes a `--min-score` parameter on all search commands. Results below the threshold are excluded from the response entirely. This allows callers to express "only give me results you are confident about" rather than "give me the top N results regardless of quality."

## Relevance

Our search API currently returns the top-N results by score. For agent use cases, a low-confidence result can be worse than no result — an agent that acts on a weakly relevant object may produce worse output than one that returns "nothing found." Agents need a way to express a relevance floor, not just a rank limit.

For human use cases, score thresholding surfaces fewer but more useful results, improving signal-to-noise in the CLI output.

## Proposed Addition

Add `min_score` as a first-class parameter on `SearchRequest` (alongside the existing `limit`):

```go
type SearchRequest struct {
    Query     string
    Quality   QualityTier
    Limit     int     // max results (default: 10)
    MinScore  float64 // 0.0 = no threshold; results below this score are excluded
    // ...
}
```

### Behavior

- Applied **after** ranking and reranking (tier-dependent)
- Applied **before** the limit — i.e., if only 3 results exceed the threshold, return 3 even if `limit=10`
- `SearchResponse` includes `filtered_count` — how many results were excluded by threshold — so callers know the corpus had matches, just not confident ones

### CLI

```bash
ctxt find "stripe webhook handling" --min-score 0.7
ctxt find "stripe webhook handling" --min-score 0.5 --best
```

### Score Normalization

Score thresholds are only meaningful if scores are normalized to a consistent range. The search response must normalize all strategy scores to [0.0, 1.0] before threshold filtering:

- BM25 scores: normalize by max BM25 score in the result set
- Vector cosine similarity: already in [-1, 1]; remap to [0, 1]
- RRF fusion scores: normalize by max RRF score in the result set
- LLM reranker scores: already normalized (reranker outputs a relevance probability)

Score normalization must be documented as part of the query engine contract.

## Default Threshold

`min_score=0.0` (no threshold) is the default — existing behavior is preserved. Users and agents opt in explicitly.

Recommended defaults for common use cases (to be surfaced in docs):
- Inline agent context assembly: `0.6`
- Composition briefs: `0.5`
- Existence checks: `0.3`

## Trade-offs

- Threshold tuning is user/agent responsibility — wrong thresholds silently return empty results
- Score scale varies by tier (BM25 alone vs. RRF vs. reranked) — normalization is critical to make `min_score` mean the same thing across tiers
- `filtered_count` in the response helps callers distinguish "no matches" from "matches below threshold"

## Open Questions

- Should we expose per-strategy thresholds (e.g., `min_lexical_score`, `min_vector_score`) or keep a single unified threshold on the final ranked list?
- Should the CLI warn when `min_score` is set but `quality=fast` (BM25-only scores are less calibrated)?
- Should score normalization be done per-tier or globally across all tiers?

## Related

- ADR-011 – Use Multi-Source Reranker (reranker outputs the most calibrated scores)
- ADR-022 – Vector Semantic Search
- ADR-061 – Search Quality Tiers (threshold behavior differs by tier due to scoring differences)
- ADR-060 – Contextual Provenance Cascade (context cascade happens before threshold filtering)
