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
