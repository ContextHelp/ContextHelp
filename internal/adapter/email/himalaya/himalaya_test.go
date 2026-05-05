package himalaya

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// TestAdapterIdentity locks the protocol and backend identifiers the
// substrate's Registry uses to enforce one-platform-per-protocol.
func TestAdapterIdentity(t *testing.T) {
	a := New(Config{})
	if got := a.Protocol(); got != "email" {
		t.Errorf("Protocol = %q, want email", got)
	}
	if got := a.Backend(); got != "himalaya" {
		t.Errorf("Backend = %q, want himalaya", got)
	}
}

// TestAdapterDeclaresFetchOnly confirms the himalaya adapter is
// capability-honest: it only declares fetch + emit-events. Server
// mode and submit are NOT in scope for the legacy himalaya wrap;
// declaring them would let substrate code mistakenly invoke methods
// that just return ErrCapabilityNotDeclared.
func TestAdapterDeclaresFetchOnly(t *testing.T) {
	a := New(Config{})
	if !adapter.HasCapability(a, adapter.CapFetch) {
		t.Error("missing CapFetch")
	}
	if !adapter.HasCapability(a, adapter.CapEmitEvents) {
		t.Error("missing CapEmitEvents (every adapter SHOULD emit lifecycle events)")
	}
	for _, c := range a.Capabilities() {
		if c == adapter.CapServe || c == adapter.CapSubmit {
			t.Errorf("unexpected capability %q on fetch-only adapter", c)
		}
	}
}

// TestAdapterRejectsUndeclaredCapabilities pins the substrate contract:
// methods for capabilities NOT declared by the adapter MUST return
// ErrCapabilityNotDeclared so substrate consumers (and policy gates)
// can branch on the sentinel via errors.Is.
func TestAdapterRejectsUndeclaredCapabilities(t *testing.T) {
	a := New(Config{})
	if err := a.Submit(context.Background(), ingest.Object{ID: "x"}); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Submit on fetch-only adapter: want ErrCapabilityNotDeclared, got %v", err)
	}
	if err := a.Serve(context.Background(), nil); !errors.Is(err, adapter.ErrCapabilityNotDeclared) {
		t.Errorf("Serve on fetch-only adapter: want ErrCapabilityNotDeclared, got %v", err)
	}
}
