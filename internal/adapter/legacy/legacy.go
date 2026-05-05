// Package legacy is the backwards-compat bridge from the typed
// internal/adapter substrate to the legacy internal/ingest.Adapter
// interface. CLI callers (cmd/ctxt/cmd/ingest.go) keep working
// unchanged during the Phase 1+2 transition window: they continue to
// see ingest.Adapter, while the underlying implementation now uses
// the typed substrate.
//
// This package lives separately from both `adapter` and `ingest` so
// the existing import direction (adapter → ingest, for ingest.Object)
// stays intact. Putting AsLegacy in either package would create an
// import cycle.
package legacy

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/internal/adapter"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
)

// AsLegacy returns a legacy ingest.Adapter view over a typed
// adapter.Adapter. The legacy Name() returns the typed adapter's
// Backend() string (matches the existing CLI source-name semantics
// for cardamum and himalaya).
//
// AsLegacy panics when the typed adapter does not declare CapFetch —
// the legacy interface has no equivalent for non-fetch capabilities,
// so silently substituting a no-op Fetch would be a confusing failure
// at the CLI surface.
func AsLegacy(typed adapter.Adapter) ingest.Adapter {
	if !adapter.HasCapability(typed, adapter.CapFetch) {
		panic("adapter/legacy: AsLegacy requires the typed adapter to declare CapFetch")
	}
	return &legacyShim{typed: typed}
}

// legacyShim is the ingest.Adapter implementation that delegates to
// the typed adapter's Fetch + Backend.
type legacyShim struct {
	typed adapter.Adapter
}

// Compile-time assertion: legacyShim satisfies the legacy contract.
var _ ingest.Adapter = (*legacyShim)(nil)

// Name returns the typed adapter's Backend() string. The legacy
// CLI's --source flag matches this value (e.g. "cardamum",
// "himalaya").
func (s *legacyShim) Name() string {
	return s.typed.Backend()
}

// Fetch delegates to the typed adapter's Fetch capability.
func (s *legacyShim) Fetch(ctx context.Context) ([]ingest.Object, error) {
	return s.typed.Fetch(ctx)
}
