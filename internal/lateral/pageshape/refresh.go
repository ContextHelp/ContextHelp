package pageshape

import "context"

// NotifyExtractionFailure invalidates the cached recipe for (domain, pattern)
// so the next pageshape.LLMClassifier.Classify call re-asks the LLM. Called
// by ibr extraction code (or any downstream consumer) when the cached
// classification yielded empty or structurally suspicious output for the
// page in question.
//
// reason is a free-text breadcrumb for observability — it is not stored
// (the cache has no metadata layer in v1) but consumers should still pass
// a meaningful value (e.g. "empty_result", "schema_mismatch") so the call
// site is greppable.
//
// Safe to call when no entry exists for the key (no-op).
func NotifyExtractionFailure(ctx context.Context, cache RecipeCache, domain, pattern, reason string) {
	_ = reason // documented; not stored in v1
	cache.Invalidate(ctx, domain, pattern)
}
