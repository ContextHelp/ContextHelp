// Package cardamum is the cardamum backend of the contacts protocol
// slot.
//
// It wraps the existing internal/ingest/cardamum CardDAV vdir fetch
// implementation in the typed Adapter interface defined by
// internal/adapter. Declared capabilities: fetch + emit-events. A
// CardDAV server-mode capability lands in Phase 2.
package cardamum

import (
	"context"
	"net"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/contacts"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	legacy "github.com/ideacrafterslabs/ctxt/internal/ingest/cardamum"
)

// Config carries optional cardamum settings. Zero-value is safe:
// missing fields fall through to the legacy package's defaults.
type Config struct {
	Account string
	Binary  string
}

// New constructs a typed cardamum Adapter for the given addressbook.
// Internally it builds the legacy ingest.cardamum.Adapter with the
// equivalent option chain.
func New(addressbook string, cfg Config) *Adapter {
	var opts []legacy.Option
	if cfg.Account != "" {
		opts = append(opts, legacy.WithAccount(cfg.Account))
	}
	if cfg.Binary != "" {
		opts = append(opts, legacy.WithBinary(cfg.Binary))
	}
	return &Adapter{legacy: legacy.New(addressbook, opts...)}
}

// Adapter is the typed cardamum adapter.
type Adapter struct {
	legacy ingest.Adapter
}

// Compile-time assertion: Adapter satisfies the typed adapter
// contract.
var _ adapter.Adapter = (*Adapter)(nil)

// Protocol returns "contacts" — the slot identity.
func (a *Adapter) Protocol() string { return contacts.Protocol }

// Backend returns "cardamum" — the canonical backend identifier.
func (a *Adapter) Backend() string { return "cardamum" }

// Capabilities returns the cardamum backend's declared capabilities:
// fetch (lists vdir cards via the cardamum CLI) and emit-events.
// Server-mode (CardDAV serving) lands in Phase 2.
func (a *Adapter) Capabilities() []adapter.Capability {
	return []adapter.Capability{adapter.CapFetch, adapter.CapEmitEvents}
}

// Start is a no-op — the legacy cardamum CLI is invoked fresh per
// Fetch, so no persistent state to set up.
func (a *Adapter) Start(_ context.Context, _ bus.Bus) error { return nil }

// Ready returns true once the legacy adapter is constructed.
func (a *Adapter) Ready() bool { return a.legacy != nil }

// Drain is a no-op (no in-flight state).
func (a *Adapter) Drain(_ context.Context) error { return nil }

// Stop is a no-op (no resources held).
func (a *Adapter) Stop(_ context.Context) error { return nil }

// Fetch delegates to the legacy implementation, which runs the
// cardamum CLI and converts cards into ingest.Object.
func (a *Adapter) Fetch(ctx context.Context) ([]ingest.Object, error) {
	return a.legacy.Fetch(ctx)
}

// Submit returns ErrCapabilityNotDeclared — cardamum is fetch-only.
func (a *Adapter) Submit(_ context.Context, _ ingest.Object) error {
	return adapter.ErrCapabilityNotDeclared
}

// Serve returns ErrCapabilityNotDeclared — CardDAV server-mode lands
// in Phase 2, not under the legacy wrap.
func (a *Adapter) Serve(_ context.Context, _ net.Listener) error {
	return adapter.ErrCapabilityNotDeclared
}
