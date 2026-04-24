package ingest

import (
	"fmt"
	"sync"
)

// Registry holds named adapter constructors.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]AdapterFactory
}

// AdapterFactory builds an Adapter from CLI-supplied args.
type AdapterFactory func(args []string) (Adapter, error)

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]AdapterFactory)}
}

// Register adds a named adapter factory.
func (r *Registry) Register(name string, f AdapterFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = f
}

// Get returns the factory for the named adapter.
func (r *Registry) Get(name string) (AdapterFactory, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("unknown adapter: %s", name)
	}
	return f, nil
}

// List returns all registered adapter names.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for k := range r.adapters {
		names = append(names, k)
	}
	return names
}
