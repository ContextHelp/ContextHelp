package registry

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// InboundGate turns the outbound registry policy vocabulary inward:
// the same EntitlementChecker namespace matcher and Meter quota logic
// that govern this instance's access to remote registries gate what an
// authenticated caller of THIS instance may read — keyed by principal
// ID instead of remote registry name.
//
// Data model reuse is deliberate (same substrate, same stores):
//
//   - an EntitlementStore row whose RegistryName is the principal ID
//     lists the namespace globs that principal is entitled to;
//   - MeteringStore events accumulate under the principal ID, and
//     per-principal QuotaConfig limits enforce hard caps per billing
//     period.
//
// A principal WITHOUT a stored grant is unrestricted — grants opt a
// principal into namespace gating. Deny-by-default (agent tags,
// per-agent context windows) attaches to this seam later.
//
// A nil *InboundGate is a no-op allow on every method, so private
// instances simply never construct one.
type InboundGate struct {
	checker *EntitlementChecker
	meter   *Meter
}

// NewInboundGate builds a gate over the instance's own entitlement and
// metering stores. The gate's meter fails CLOSED: gates only exist on
// non-private instances, where a quota-check failure must deny the
// access rather than serve it unmetered.
func NewInboundGate(ents storage.EntitlementStore, met storage.MeteringStore) *InboundGate {
	meter := NewMeter(met)
	meter.SetFailClosed(true)
	return &InboundGate{
		checker: NewEntitlementChecker(ents),
		meter:   meter,
	}
}

// SetQuota registers a hard limit for one principal + event type.
// Limit 0 means unlimited. Call during wiring, before serving.
func (g *InboundGate) SetQuota(principal string, event storage.MeteringEventType, cfg storage.QuotaConfig) {
	if g == nil {
		return
	}
	g.meter.SetQuota(principal, event, cfg)
}

// Check verifies principal's namespace entitlement without metering —
// the read path for filtering index listings. Returns
// ErrEntitlementRequired when a stored grant excludes namespace.
func (g *InboundGate) Check(ctx context.Context, principal, namespace string) error {
	if g == nil {
		return nil
	}
	return g.checker.CheckNamespace(ctx, principal, namespace, "")
}

// Authorize gates one billable access: the entitlement check first,
// then a metered quota charge for the current billing period. Returns
// ErrEntitlementRequired or ErrQuotaExhausted unchanged so transports
// can map them onto their own status codes. Denied accesses are never
// metered.
func (g *InboundGate) Authorize(ctx context.Context, principal, namespace string, event storage.MeteringEventType) error {
	if g == nil {
		return nil
	}
	if err := g.checker.CheckNamespace(ctx, principal, namespace, ""); err != nil {
		return err
	}
	return g.meter.RecordEvent(ctx, principal, event, namespace, billingPeriodStart(time.Now()))
}

// billingPeriodStart returns the start of the calendar month containing
// now, in UTC — the quota window usage is summed over.
func billingPeriodStart(now time.Time) time.Time {
	y, m, _ := now.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}
