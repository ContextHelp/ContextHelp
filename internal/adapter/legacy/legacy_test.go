package legacy

import (
	"context"
	"net"
	"testing"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// fakeTyped is a minimal typed adapter used to verify the shim
// presents the legacy ingest.Adapter surface. Real adapters
// (himalaya, cardamum) come with their own tests under their slot
// directories; this fixture is shim-shaped.
type fakeTyped struct {
	protocol string
	backend  string
	objects  []ingest.Object
}

func (f *fakeTyped) Protocol() string { return f.protocol }
func (f *fakeTyped) Backend() string  { return f.backend }
func (f *fakeTyped) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch}
}
func (f *fakeTyped) Start(_ context.Context, _ bus.Bus) error         { return nil }
func (f *fakeTyped) Ready() bool                                      { return true }
func (f *fakeTyped) Drain(_ context.Context) error                    { return nil }
func (f *fakeTyped) Stop(_ context.Context) error                     { return nil }
func (f *fakeTyped) Fetch(_ context.Context) ([]ingest.Object, error) { return f.objects, nil }
func (f *fakeTyped) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}
func (f *fakeTyped) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}

// TestAsLegacyForwardsName confirms the shim exposes the typed
// adapter's Backend() through the legacy Name() method (matches the
// existing CLI's source-name semantics where Name returned
// "cardamum"/"himalaya").
func TestAsLegacyForwardsName(t *testing.T) {
	typed := &fakeTyped{protocol: "email", backend: "test"}
	legacy := AsLegacy(typed)
	if got := legacy.Name(); got != "test" {
		t.Errorf("legacy.Name() = %q, want %q (typed.Backend())", got, "test")
	}
}

// TestAsLegacyForwardsFetch confirms Fetch round-trips through the
// shim without copy/conversion losses.
func TestAsLegacyForwardsFetch(t *testing.T) {
	typed := &fakeTyped{
		protocol: "email",
		backend:  "test",
		objects:  []ingest.Object{{ID: "1", Content: "hi"}, {ID: "2", Content: "bye"}},
	}
	legacy := AsLegacy(typed)
	got, err := legacy.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 2 || got[0].ID != "1" || got[1].ID != "2" {
		t.Errorf("Fetch returned %v, want 2 objects with IDs 1,2", got)
	}
}

// TestAsLegacyPanicsWithoutCapFetch is the substrate's loud failure
// mode — a typed adapter that doesn't declare CapFetch can't be
// presented as a legacy fetch-only ingest.Adapter.
func TestAsLegacyPanicsWithoutCapFetch(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("AsLegacy on a non-fetch adapter must panic")
		}
	}()
	noFetch := &fakeTypedNoFetch{}
	_ = AsLegacy(noFetch)
}

// fakeTypedNoFetch declares CapServe instead of CapFetch.
type fakeTypedNoFetch struct{ fakeTyped }

func (f *fakeTypedNoFetch) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapServe}
}
