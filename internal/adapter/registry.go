package adapter

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrProtocolAlreadyRegistered indicates an attempt to register a
// second adapter for a protocol slot that already has one. Per
// ADR-065: one platform per protocol per dPKMS instance;
// multi-platform deployments compose via ADR-064 federation, not
// multi-backend slots. Registry.Register wraps this sentinel with
// the specific protocol + existing-backend names so operators see
// what's already configured.
var ErrProtocolAlreadyRegistered = errors.New("adapter: protocol already registered")

// Registry holds at most one Adapter per protocol slot. The
// daemon constructs a single Registry at startup and the runner
// drives lifecycle for every registered adapter.
type Registry struct {
	mu    sync.RWMutex
	slots map[string]Adapter
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{slots: make(map[string]Adapter)}
}

// Register adds a under its declared Protocol. Returns
// ErrProtocolAlreadyRegistered (wrapped with operator-facing context)
// when a different adapter is already registered for the same
// protocol slot.
func (r *Registry) Register(a Adapter) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	proto := a.Protocol()
	if existing, ok := r.slots[proto]; ok {
		return fmt.Errorf("%w: protocol %q already configured with backend %q (use ADR-064 federation for multi-platform)",
			ErrProtocolAlreadyRegistered, proto, existing.Backend())
	}
	r.slots[proto] = a
	return nil
}

// Get returns the adapter for the given protocol slot, or nil if no
// adapter is registered for that protocol. Callers that need an error
// surface (e.g. CLI reporting) wrap the nil return themselves.
func (r *Registry) Get(protocol string) Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.slots[protocol]
}

// Protocols returns the sorted list of registered protocol slots.
// Sorted for stable iteration in operator surfaces (`dpkms adapter
// list`, log lines).
func (r *Registry) Protocols() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.slots))
	for p := range r.slots {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
