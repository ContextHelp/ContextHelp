package http

import (
	"errors"
	"net/http"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

// gatePrincipal returns the authenticated principal ID for inbound
// entitlement checks. Behind RequireAuth it is always present; the
// empty fallback keeps ungated (private) routers working unchanged.
func gatePrincipal(r *http.Request) string {
	if p, ok := authn.FromContext(r.Context()); ok {
		return p.ID
	}
	return ""
}

// entityGate returns the inbound gate that applies to this request:
// nil (no entitlement filter, no metering) for a principal that
// bypasses it, the admin owner of the instance; gate otherwise. The
// gate exists for third-party consumers, not the owner.
func entityGate(r *http.Request, gate *registry.InboundGate) *registry.InboundGate {
	if p, ok := authn.FromContext(r.Context()); ok && p.BypassesEntityGate() {
		return nil
	}
	return gate
}

// writeInboundGateError maps InboundGate authorization errors onto the
// HTTP surface: ErrEntitlementRequired → 403, ErrQuotaExhausted → 429.
// Each denial is recorded as a security event against the principal
// via the router-scoped emitter. Returns true when err was handled;
// other errors stay with the caller's own error path.
func writeInboundGateError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	principal := gatePrincipal(r)
	sec := securityEmitterFrom(r.Context())
	switch {
	case registry.IsEntitlementRequired(err):
		if sec != nil {
			sec.RecordACLDenial(r.Context(), principal)
		}
		WriteError(w, http.StatusForbidden, "ENTITLEMENT_REQUIRED", err.Error())
		return true
	case errors.Is(err, registry.ErrQuotaExhausted):
		if sec != nil {
			sec.RecordQuotaExhausted(r.Context(), principal, principal)
		}
		WriteError(w, http.StatusTooManyRequests, "QUOTA_EXHAUSTED", err.Error())
		return true
	}
	return false
}
