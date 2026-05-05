// Package adapter is the typed pluggable adapter substrate per
// ADR-065. Adapters implement protocol slots (email, contacts,
// calendar, ...) with declared capabilities (fetch, serve, submit,
// emit-events, subscribe-events) and a Start/Ready/Drain/Stop
// lifecycle. The substrate enforces one-platform-per-protocol per
// dPKMS instance; multi-platform deployments compose via ADR-064
// federation, not multi-backend slots.
//
// Existing fetch-only adapters (cardamum, himalaya) live as backends
// under the email/contacts slots and continue to satisfy the legacy
// internal/ingest.Adapter interface via internal/ingest.AsLegacy.
package adapter

import (
	"context"
	"net"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// Capability is a declared adapter capability. The substrate gates
// which methods are callable based on declared capabilities;
// undeclared capability methods MUST return ErrCapabilityNotDeclared.
type Capability string

const (
	// CapFetch declares the adapter can pull objects from an external
	// source (Adapter.Fetch is callable).
	CapFetch Capability = "fetch"
	// CapServe declares the adapter exposes a wire protocol to clients
	// (Adapter.Serve is callable; long-running daemon).
	CapServe Capability = "serve"
	// CapSubmit declares the adapter can push objects to an external
	// sink (Adapter.Submit is callable).
	CapSubmit Capability = "submit"
	// CapEmitEvents declares the adapter publishes pre-* and post-*
	// events on the bus around its mutations. All adapters SHOULD
	// declare this — kit/runtime/policy depends on the pre-event seam
	// to gate operations.
	CapEmitEvents Capability = "emit-events"
	// CapSubscribeEvents declares the adapter consumes events from the
	// bus (typically other adapters' post-events) to react to shared
	// state changes.
	CapSubscribeEvents Capability = "subscribe-events"
)

// LifecycleState is the adapter's current lifecycle position. The
// runner emits dpkms.adapter.lifecycle.<state> on every transition.
type LifecycleState string

const (
	// StateStopped is the initial and final state. The adapter is
	// inert: capability-gated methods return errors; bus subscriptions
	// (if any) are detached.
	StateStopped LifecycleState = "stopped"
	// StateStarting is transient — Start has been entered but Ready
	// has not yet returned true.
	StateStarting LifecycleState = "starting"
	// StateReady means the adapter is fully initialized and its
	// declared capabilities are usable.
	StateReady LifecycleState = "ready"
	// StateDraining is transient — the runner has begun shutdown; the
	// adapter is finishing in-flight work and rejecting new work.
	StateDraining LifecycleState = "draining"
)

// Adapter is the typed pluggable adapter contract.
//
// Identity (Protocol/Backend/Capabilities) MUST be stable across
// the adapter's lifetime — registration depends on it.
//
// Lifecycle methods (Start/Ready/Drain/Stop) are always called by
// the runner. Capability-gated methods (Fetch/Submit/Serve) are only
// invoked when the corresponding Capability is declared. Implementations
// of capability-gated methods that are NOT declared MUST return
// ErrCapabilityNotDeclared so substrate misuse fails loud.
type Adapter interface {
	// Identity
	Protocol() string // "email", "contacts", "calendar", ...
	Backend() string  // "stalwart", "mxhook+gmail", "cardamum", ...
	Capabilities() []Capability

	// Lifecycle
	Start(ctx context.Context, b bus.Bus) error
	Ready() bool
	Drain(ctx context.Context) error
	Stop(ctx context.Context) error

	// Capability-gated. Implementations without the corresponding
	// Capability MUST return ErrCapabilityNotDeclared.
	Fetch(ctx context.Context) ([]ingest.Object, error)
	Submit(ctx context.Context, obj ingest.Object) error
	Serve(ctx context.Context, listener net.Listener) error
}

// HasCapability reports whether a declares c. Substrate code uses it
// before invoking capability-gated methods; adapter authors should
// declare all and only the capabilities they actually implement.
func HasCapability(a Adapter, c Capability) bool {
	for _, x := range a.Capabilities() {
		if x == c {
			return true
		}
	}
	return false
}
