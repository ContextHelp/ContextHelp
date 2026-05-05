package adapter

import (
	"testing"
)

// TestCapabilitiesAreDistinct guards against accidental duplicate-string
// definitions when new Capability constants are added. The substrate
// uses the string value as the key for capability gating; collisions
// would silently allow undeclared methods.
func TestCapabilitiesAreDistinct(t *testing.T) {
	caps := []Capability{
		CapFetch, CapServe, CapSubmit, CapEmitEvents, CapSubscribeEvents,
	}
	seen := make(map[Capability]bool, len(caps))
	for _, c := range caps {
		if seen[c] {
			t.Fatalf("duplicate capability: %q", c)
		}
		seen[c] = true
	}
}

// TestLifecycleStatesAreDistinct guards against accidental duplicate
// LifecycleState string definitions. The runner publishes the state
// name on the bus; collisions would conflate transitions.
func TestLifecycleStatesAreDistinct(t *testing.T) {
	states := []LifecycleState{
		StateStopped, StateStarting, StateReady, StateDraining,
	}
	seen := make(map[LifecycleState]bool, len(states))
	for _, s := range states {
		if seen[s] {
			t.Fatalf("duplicate state: %q", s)
		}
		seen[s] = true
	}
}
