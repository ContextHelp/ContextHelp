package ambient

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the set of ambient Sources active in a Runner. Names are
// unique; multiple Sources of the same Name() are not allowed (in contrast to
// ADR-065 typed adapters, where the invariant is one-platform-per-protocol).
//
// Registry is safe for concurrent use; the Runner reads it from its dispatch
// goroutine while CLI / status callers may read concurrently.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]Source
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]Source)}
}

// Register adds a Source under its declared Name(). Returns
// ErrSourceAlreadyRegistered when a Source with the same Name() is already
// registered; callers should treat this as configuration error.
func (r *Registry) Register(s Source) error {
	if s == nil {
		return fmt.Errorf("ambient: nil source")
	}
	name := s.Name()
	if name == "" {
		return fmt.Errorf("ambient: source has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sources[name]; ok {
		return fmt.Errorf("%w: %q", ErrSourceAlreadyRegistered, name)
	}
	r.sources[name] = s
	return nil
}

// Get returns the Source registered under name, or nil if no such Source.
func (r *Registry) Get(name string) Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[name]
}

// Names returns the sorted list of registered Source names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.sources))
	for n := range r.sources {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of registered sources.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sources)
}
