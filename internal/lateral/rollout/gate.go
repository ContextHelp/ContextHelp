package rollout

import (
	"sync"
	"sync/atomic"
)

// StrategyGate is a runtime per-strategy enabled flag, mutable via
// Set / SetAll. Reads (Allowed) are lock-free for the hot dispatch
// path; writes serialize on a mutex.
//
// Zero value is "every strategy allowed" — Allowed returns true for
// every ID until at least one Set / SetAll happens.
type StrategyGate struct {
	mu      sync.Mutex
	enabled atomic.Pointer[map[string]bool] // nil = all-allowed
}

// NewStrategyGate returns an empty gate (allows every ID).
func NewStrategyGate() *StrategyGate { return &StrategyGate{} }

// Allowed reports whether dispatch should proceed for strategy id.
// A nil gate or zero-value gate returns true (the "no operator
// override yet" path).
func (g *StrategyGate) Allowed(id string) bool {
	if g == nil {
		return true
	}
	m := g.enabled.Load()
	if m == nil {
		return true
	}
	v, ok := (*m)[id]
	if !ok {
		return true
	}
	return v
}

// Set toggles a single strategy. Subsequent Allowed calls reflect
// the change atomically.
func (g *StrategyGate) Set(id string, allowed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	cur := g.enabled.Load()
	next := map[string]bool{}
	if cur != nil {
		for k, v := range *cur {
			next[k] = v
		}
	}
	next[id] = allowed
	g.enabled.Store(&next)
}

// SetAll replaces the gate's entire map atomically. The new map is
// copied so callers can mutate their local map without racing future
// Set calls.
func (g *StrategyGate) SetAll(flags map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	next := make(map[string]bool, len(flags))
	for k, v := range flags {
		next[k] = v
	}
	g.enabled.Store(&next)
}

// Snapshot returns a copy of the gate's current flag map. Useful for
// status subcommand output and for tests asserting reload semantics.
func (g *StrategyGate) Snapshot() map[string]bool {
	cur := g.enabled.Load()
	if cur == nil {
		return nil
	}
	out := make(map[string]bool, len(*cur))
	for k, v := range *cur {
		out[k] = v
	}
	return out
}
