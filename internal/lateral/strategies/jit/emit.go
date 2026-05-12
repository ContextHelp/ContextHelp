package jit

import lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"

// CandidateType is the lateral.Candidate.CandidateType value emitted by JIT.
// Single value (vs platform strategies' relationship-typed taxonomy) because
// JIT proposes by domain, not by structural relationship — cap-gate config
// targets this constant when sizing JIT-specific thresholds and caps.
const CandidateType = "jit_subpath"

// Emit converts a slice of FetchResult into lateral.Candidate values, dropping
// failed fetches into a separate failures slice for T-0248 to consume.
//
// Emission rules:
//   - results with non-nil Err → failures slice (NOT a candidate)
//   - results with nil Err and non-empty URL → one candidate with
//     CandidateType=jit_subpath, Strategy=StrategyID, Preview carrying
//     page_type and body_size signals
//   - empty body is preserved (zero body_size) — the upstream fetch succeeded
//     so it's still a valid candidate; downstream scorer decides what to do
//
// pageType is the proposer-input label (e.g. "post", "doc"), passed through
// into Preview so cap-gate / scorer have a per-page-type signal without
// re-deriving.
func Emit(results []FetchResult, pageType string) (candidates []lateral.Candidate, failures []FetchResult) {
	if len(results) == 0 {
		return nil, nil
	}
	candidates = make([]lateral.Candidate, 0, len(results))
	failures = make([]FetchResult, 0)

	for _, r := range results {
		if r.Err != nil {
			failures = append(failures, r)
			continue
		}
		if r.URL == "" {
			// Defensive: an executor should never emit a result with empty
			// URL on success. Skip rather than emit a malformed candidate.
			continue
		}
		candidates = append(candidates, lateral.Candidate{
			URL:           r.URL,
			CandidateType: CandidateType,
			Strategy:      StrategyID,
			Preview: map[string]any{
				"page_type": pageType,
				"body_size": len(r.Body),
			},
		})
	}

	if len(candidates) == 0 {
		candidates = nil
	}
	if len(failures) == 0 {
		failures = nil
	}
	return candidates, failures
}
