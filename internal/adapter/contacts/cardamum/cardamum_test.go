package cardamum

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
)

// TestAdapterIdentity locks the protocol and backend identifiers the
// Registry uses for one-platform-per-protocol routing.
func TestAdapterIdentity(t *testing.T) {
	a := New("default", Config{})
	if got := a.Protocol(); got != "contacts" {
		t.Errorf("Protocol = %q, want contacts", got)
	}
	if got := a.Backend(); got != "cardamum" {
		t.Errorf("Backend = %q, want cardamum", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms the cardamum adapter is
// capability-honest. CardDAV server mode is a Phase 2 deliverable;
// declaring CapServe today would let substrate code mistakenly call
// methods that return ErrCapabilityNotDeclared.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New("default", Config{})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents")
	}
	if adapter.HasCapability(a, adapter.CapServe) {
		t.Error("unexpected CapServe — server-mode lands in Phase 2")
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract:
// methods for capabilities NOT declared MUST return
// ErrCapabilityNotDeclared (errors.Is matches).
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New("default", Config{})
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve: want ErrCapabilityNotDeclared, got %v", err)
	}
}
