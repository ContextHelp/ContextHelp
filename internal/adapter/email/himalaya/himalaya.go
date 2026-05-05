// Package himalaya is the himalaya backend of the email protocol slot.
//
// It wraps the existing internal/ingest/himalaya fetch implementation
// in the typed Adapter interface defined by internal/adapter. The
// declared capabilities are CapFetch + CapEmitEvents — submit and
// serve land later (Phase 2+ when bidirectional + protocol-server
// adapters arrive).
package himalaya

import (
	"context"
	"net"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/email"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	legacy "github.com/ideacrafterslabs/ctxt/internal/ingest/himalaya"
)

// Config carries the himalaya invocation settings. Zero-value is safe;
// all fields are optional and let the legacy himalaya implementation
// fall back to its defaults.
type Config struct {
	Account  string
	Folder   string
	MaxItems int
	Binary   string
}

// New constructs a typed himalaya Adapter. The legacy
// internal/ingest/himalaya.Adapter handles the actual `himalaya
// envelope list` invocation and JSON parsing.
func New(cfg Config) *Adapter {
	return &Adapter{
		legacy: &legacy.Adapter{
			Account:  cfg.Account,
			Folder:   cfg.Folder,
			MaxItems: cfg.MaxItems,
			Binary:   cfg.Binary,
		},
	}
}

// Adapter is the typed himalaya adapter. Lifecycle methods are
// no-op for fetch-only adapters: the legacy CLI semantics (one
// `dpkms ingest --source himalaya` invocation per shell call) maps
// to a single Fetch call after a no-op Start. Phase 2's bidirectional
// adapters will use Start/Drain/Stop for connection state.
type Adapter struct {
	legacy *legacy.Adapter
}

// Compile-time assertion: Adapter satisfies the typed adapter contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "email" — the slot identity defined in
// internal/adapter/email/slot.go. The substrate's Registry routes
// registration by this value.
func (a *Adapter) Protocol() string { return email.Protocol }

// Backend returns "himalaya" — the canonical backend name used in
// operator surfaces (`dpkms adapter list`, log lines, error messages
// from Registry duplicate-protocol rejection).
func (a *Adapter) Backend() string { return "himalaya" }

// Capabilities returns the himalaya backend's declared capabilities:
// fetch (pulls from IMAP via the himalaya CLI) and emit-events
// (lifecycle topics on the bus, driven by the Runner).
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op for the legacy himalaya wrap — every Fetch invokes
// the binary fresh, so there's no persistent connection to set up.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error { return nil }

// Ready returns true once the legacy adapter is constructed.
func (a *Adapter) Ready() bool { return a.legacy != nil }

// Drain is a no-op for the legacy himalaya wrap (no in-flight state).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop is a no-op for the legacy himalaya wrap (no resources held).
func (a *Adapter) Stop(_ context.Context) error { return nil }

// Fetch delegates to the legacy implementation, which runs `himalaya
// envelope list -o json` and converts the result into ingest.Object.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	return a.legacy.Fetch(ctx)
}

// Submit returns ErrCapabilityNotDeclared — the himalaya backend is
// fetch-only. Outbound mail will land via mxhook backends in Phase 2.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — protocol-server semantics
// (Stalwart serving IMAP) live in their own backend, not here.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
