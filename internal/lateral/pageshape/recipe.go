package pageshape

import (
	"context"
	"sync"
)

// RecipeCache stores LLM-produced page-shape verdicts keyed by the
// (domain, pattern) tuple from normalize(). Implementations may be in-memory
// (MemoryRecipeCache, default) or persistent. Get returns ok=false on miss;
// Put overwrites; Invalidate is the failure-driven refresh hook used by T23.
type RecipeCache interface {
	Get(ctx context.Context, domain, pattern string) ([]Label, bool)
	Put(ctx context.Context, domain, pattern string, verdict []Label)
	Invalidate(ctx context.Context, domain, pattern string)
}

// MemoryRecipeCache is the default in-process recipe cache. Safe for
// concurrent use. Loses contents on process restart — a sqlite-backed
// variant is a follow-on for deployments that want persistence.
type MemoryRecipeCache struct {
	mu sync.RWMutex
	m  map[string][]Label
}

// NewMemoryRecipeCache constructs an empty in-memory cache.
func NewMemoryRecipeCache() *MemoryRecipeCache {
	return &MemoryRecipeCache{m: map[string][]Label{}}
}

func (c *MemoryRecipeCache) key(domain, pattern string) string { return domain + "|" + pattern }

// Get returns the cached verdict for (domain, pattern) and ok=true on hit.
func (c *MemoryRecipeCache) Get(_ context.Context, domain, pattern string) ([]Label, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[c.key(domain, pattern)]
	return v, ok
}

// Put stores verdict for (domain, pattern), overwriting any prior value.
func (c *MemoryRecipeCache) Put(_ context.Context, domain, pattern string, verdict []Label) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[c.key(domain, pattern)] = verdict
}

// Invalidate removes the entry for (domain, pattern). T23's failure-driven
// refresh calls this when a cached recipe produces a verdict that conflicts
// with downstream signals.
func (c *MemoryRecipeCache) Invalidate(_ context.Context, domain, pattern string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.m, c.key(domain, pattern))
}
