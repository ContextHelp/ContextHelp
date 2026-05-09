package jit

import "context"

// CachedProposer composes a Proposer with a ProposalCache: cache hit
// short-circuits the proposer call; cache miss calls the proposer and stores
// the verdict keyed by (domain, pageType). Mirrors pageshape.LLMClassifier
// exactly so the failure-driven invalidate pattern (see pageshape.refresh)
// transfers cleanly.
//
// Empty proposer results are NOT cached — a transient LLM failure or "no
// opinion" verdict shouldn't stick. The next call retries.
type CachedProposer struct {
	proposer Proposer
	cache    ProposalCache
}

// NewCachedProposer wires a CachedProposer to its proposer and cache.
func NewCachedProposer(p Proposer, c ProposalCache) *CachedProposer {
	return &CachedProposer{proposer: p, cache: c}
}

// Propose returns sub-paths for (domain, pageType). On cache miss the
// underlying proposer is consulted and a non-empty verdict is stored before
// returning. Proposer errors propagate; cache reads/writes are infallible
// for the in-memory adapter.
func (c *CachedProposer) Propose(ctx context.Context, domain, pageType string) ([]string, error) {
	if v, ok := c.cache.Get(ctx, domain, pageType); ok {
		return v, nil
	}
	v, err := c.proposer.Propose(ctx, domain, pageType)
	if err != nil {
		return nil, err
	}
	if len(v) > 0 {
		c.cache.Put(ctx, domain, pageType, v)
	}
	return v, nil
}

// ProposeWithOutageFallback is the outage-aware variant of Propose. It
// returns the same sub-paths Propose would, plus a served flag indicating
// whether the caller should emit ctxt.lateral.recipe.served with
// Circumstance=llm_outage.
//
// served=true ONLY when both:
//   - the cache had a prior verdict for (domain, pageType), AND
//   - det reports an active outage (IsOutage()==true)
//
// In all other cases served=false. A nil det is treated as "no outage."
//
// Truth table:
//   - cache hit + outage   → (cached, true,  nil)   serve cached, fire event
//   - cache hit + healthy  → (cached, false, nil)   serve cached, no event
//   - miss + proposer ok   → (fresh,  false, nil)   normal fetch, cache populated
//   - miss + proposer err  → (nil,    false, err)   pure failure, no event
//
// The detector is queried only on cache hit because the outage event
// describes what was served — a cache miss that succeeds isn't a "served
// during outage" event regardless of detector state, and a cache miss that
// fails is a scan.failed event handled by EmitProposalFailure.
func (c *CachedProposer) ProposeWithOutageFallback(ctx context.Context, domain, pageType string, det OutageDetector) (subpaths []string, served bool, err error) {
	if v, ok := c.cache.Get(ctx, domain, pageType); ok {
		if det != nil && det.IsOutage() {
			return v, true, nil
		}
		return v, false, nil
	}
	v, perr := c.proposer.Propose(ctx, domain, pageType)
	if perr != nil {
		return nil, false, perr
	}
	if len(v) > 0 {
		c.cache.Put(ctx, domain, pageType, v)
	}
	return v, false, nil
}
