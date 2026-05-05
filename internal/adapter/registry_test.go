package adapter

import (
	"context"
	"errors"
	"net"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// stubAdapter is a minimal Adapter for registry tests. Real adapter
// tests live alongside their implementations under
// internal/adapter/<protocol>/<backend>/.
type stubAdapter struct {
	protocol string
	backend  string
}

func (s *stubAdapter) Protocol() string                                 { return s.protocol }
func (s *stubAdapter) Backend() string                                  { return s.backend }
func (s *stubAdapter) Capabilities() []Capability                       { return []Capability{CapFetch} }
func (s *stubAdapter) Start(_ context.Context, _ bus.Bus) error         { return nil }
func (s *stubAdapter) Ready() bool                                      { return true }
func (s *stubAdapter) Drain(_ context.Context) error                    { return nil }
func (s *stubAdapter) Stop(_ context.Context) error                     { return nil }
func (s *stubAdapter) Fetch(_ context.Context) ([]ingest.Object, error) { return nil, nil }
func (s *stubAdapter) Submit(_ context.Context, _ ingest.Object) error {
	return ErrCapabilityNotDeclared
}
func (s *stubAdapter) Serve(_ context.Context, _ net.Listener) error { return ErrCapabilityNotDeclared }

// TestRegistryRegistersAdapter exercises the happy path: a single
// adapter goes in, comes back via Get under its declared protocol.
func TestRegistryRegistersAdapter(t *testing.T) {
	r := NewRegistry()
	a := &stubAdapter{protocol: "email", backend: "himalaya"}
	if err := r.Register(a); err != nil {
		t.Fatalf("Register: unexpected error %v", err)
	}
	if got := r.Get("email"); got == nil {
		t.Fatal("Get(email) returned nil after Register")
	}
}

// TestRegistryRejectsDuplicateProtocol locks the one-platform-per-protocol
// invariant from ADR-065. Multi-platform deployments compose via
// ADR-064 federation, not multi-backend slots.
func TestRegistryRejectsDuplicateProtocol(t *testing.T) {
	r := NewRegistry()
	a := &stubAdapter{protocol: "email", backend: "himalaya"}
	b := &stubAdapter{protocol: "email", backend: "stalwart"}
	if err := r.Register(a); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(b)
	if !errors.Is(err, ErrProtocolAlreadyRegistered) {
		t.Fatalf("second Register: want ErrProtocolAlreadyRegistered, got %v", err)
	}
}

// TestRegistryProtocolsReturnsSorted gives ops a stable iteration
// order for `dpkms adapter list`-style surfaces.
func TestRegistryProtocolsReturnsSorted(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&stubAdapter{protocol: "email", backend: "himalaya"})
	_ = r.Register(&stubAdapter{protocol: "contacts", backend: "cardamum"})
	got := r.Protocols()
	if len(got) != 2 {
		t.Fatalf("Protocols: want 2, got %d (%v)", len(got), got)
	}
	if got[0] != "contacts" || got[1] != "email" {
		t.Errorf("Protocols: want sorted [contacts email], got %v", got)
	}
}

// TestRegistryGetMissingReturnsNil gives consumers a clear "no such
// slot" signal without an error allocation.
func TestRegistryGetMissingReturnsNil(t *testing.T) {
	r := NewRegistry()
	if got := r.Get("calendar"); got != nil {
		t.Errorf("Get on empty registry: want nil, got %v", got)
	}
}
