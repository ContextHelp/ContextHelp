package adapter

import (
	"errors"
	"fmt"
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

// TestErrCapabilityNotDeclared_IsSentinel proves the package-level
// error supports errors.Is matching when wrapped — capability-gated
// adapter methods that aren't declared MUST return this sentinel so
// substrate code can branch on it without parsing error strings.
func TestErrCapabilityNotDeclared_IsSentinel(t *testing.T) {
	if ErrCapabilityNotDeclared == nil {
		t.Fatal("ErrCapabilityNotDeclared must be a non-nil sentinel")
	}
	wrapped := fmt.Errorf("adapter foo: %w", ErrCapabilityNotDeclared)
	if !errors.Is(wrapped, ErrCapabilityNotDeclared) {
		t.Fatal("ErrCapabilityNotDeclared must support errors.Is when wrapped")
	}
}
