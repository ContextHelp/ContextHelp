package jit

import (
	"context"
	"sync"
)

// ProposalCache stores LLM-produced JIT sub-path proposals keyed by the
// (source_domain, page_type) tuple. page_type is a string label assigned by
// the JIT proposer (e.g. "repo_root", "issue_list", "doc_section"), not a
// URL path pattern.
//
// Sibling of pageshape.RecipeCache, not a reuse: pageshape keys on
// (domain, URL pattern) and stores []pageshape.Label, JIT keys on
// (domain, page_type) and stores []string sub-paths. Sharing the same
// instance would collide keys and erase the value type. The shape (Get/Put/
// Invalidate, MemoryX impl, sync.RWMutex, "domain|key" composition) is
// mirrored deliberately so the failure-driven invalidate pattern from
// pageshape.NotifyExtractionFailure transfers cleanly.
//
// Implementations may be in-memory (MemoryProposalCache, default) or
// persistent. Get returns ok=false on miss; Put overwrites; Invalidate
// removes the entry (no-op when absent).
type ProposalCache interface {
	Get(ctx context.Context, domain, pageType string) ([]string, bool)
	Put(ctx context.Context, domain, pageType string, subpaths []string)
	Invalidate(ctx context.Context, domain, pageType string)
}

// MemoryProposalCache is the default in-process JIT proposal cache. Safe
// for concurrent use. Loses contents on process restart — a persistent
// variant is a follow-on for deployments that want recipe survival across
// restarts.
type MemoryProposalCache struct {
	mu sync.RWMutex
	m  map[string][]string
}

// NewMemoryProposalCache constructs an empty in-memory proposal cache.
func NewMemoryProposalCache() *MemoryProposalCache {
	return &MemoryProposalCache{m: map[string][]string{}}
}

func (c *MemoryProposalCache) key(domain, pageType string) string {
	return domain + "|" + pageType
}

// Get returns the cached sub-paths for (domain, pageType) and ok=true on hit.
func (c *MemoryProposalCache) Get(_ context.Context, domain, pageType string) ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[c.key(domain, pageType)]
	return v, ok
}

// Put stores subpaths for (domain, pageType), overwriting any prior value.
func (c *MemoryProposalCache) Put(_ context.Context, domain, pageType string, subpaths []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[c.key(domain, pageType)] = subpaths
}

// Invalidate removes the entry for (domain, pageType). Safe to call when no
// entry exists for the key (no-op).
func (c *MemoryProposalCache) Invalidate(_ context.Context, domain, pageType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, c.key(domain, pageType))
}
