package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Sharing changes by non-owner administrators (ADR-083 §10 B). Every
// route that changes the sharing registry builds its change and hands
// it to sharechange.Service: the owner's change is written at once; any
// other caller's is classified against the registry in the same
// transaction as the write, and one that widens anyone's view, or that
// cannot be shown not to, is answered 202 with a pending share.change
// request and writes nothing until an approver other than the requester
// and the people it benefits accepts it.

// writeKind says how a mutating route of the principal and sharing
// registries meets that rule.
type writeKind string

const (
	// writeShareChange: the route's change is classified and, when it
	// widens, filed (sharechange.Service.Submit).
	writeShareChange writeKind = "share_change"
	// writeOwnBinding: a principal's change to its own key's policy. A
	// key's view never exceeds its principal's, so it is not an admin
	// change and applies at once whatever its class
	// (sharechange.Service.Direct).
	writeOwnBinding writeKind = "own_binding"
	// writeCredential: a credential for another principal (ADR-083
	// §10 A): only the owner issues it (guardCredentialDelivery).
	writeCredential writeKind = "credential"
	// writeTakesAway: only takes something away (suspend, revoke, remove
	// a key or an identity); applies at once for every admin.
	writeTakesAway writeKind = "takes_away"
	// writeNoView: creates or changes nothing a viewer is compiled from
	// (a new principal has no credential and, roles never deciding a
	// viewer, no view).
	writeNoView writeKind = "no_view"
	// writeSelf: the caller's own credentials and sessions (write:self).
	writeSelf writeKind = "self"
)

// registryWrite classifies one mutating route.
type registryWrite struct {
	kind writeKind
	// action is the sharing.Action* the route's change carries, for the
	// routes that classify one.
	action string
}

// registryWrites classifies every mutating route of the principal
// registry (/principals), the sharing registry (/share) and a
// principal's own credentials (/self), keyed by routeKey. The route
// table test fails for a route that is missing here, and for an entry
// that names no route: a registry write route lands with its class or
// not at all.
var registryWrites = map[string]registryWrite{
	"POST /principals":                                       {kind: writeNoView},
	"POST /principals/{id}/suspend":                          {kind: writeTakesAway},
	"POST /principals/{id}/revoke":                           {kind: writeTakesAway},
	"POST /principals/{id}/links":                            {kind: writeCredential},
	"POST /principals/{id}/identities":                       {kind: writeCredential},
	"DELETE /principals/{id}/identities/{identity}":          {kind: writeTakesAway},
	"POST /principals/{id}/keys":                             {kind: writeCredential},
	"DELETE /principals/{id}/keys/{key}":                     {kind: writeTakesAway},
	"PUT /principals/{id}/ceiling":                           {kind: writeShareChange, action: sharing.ActionCeilingSet},
	"POST /share/invites":                                    {kind: writeCredential},
	"POST /share/audiences":                                  {kind: writeShareChange, action: sharing.ActionAudienceCreate},
	"PUT /share/audiences/{audience}/members/{principal}":    {kind: writeShareChange, action: sharing.ActionMemberPut},
	"DELETE /share/audiences/{audience}/members/{principal}": {kind: writeShareChange, action: sharing.ActionMemberRemove},
	"POST /share/policies":                                   {kind: writeShareChange, action: sharing.ActionPolicyCreate},
	"PUT /share/policies/{policy}":                           {kind: writeShareChange, action: sharing.ActionPolicyUpdate},
	"POST /share/templates/{name}":                           {kind: writeShareChange, action: sharing.ActionTemplateInstall},
	"POST /share/grants":                                     {kind: writeShareChange, action: sharing.ActionGrantCreate},
	"DELETE /share/grants/{grant}":                           {kind: writeShareChange, action: sharing.ActionGrantRevoke},
	"PUT /share/keys/{key}/policy":                           {kind: writeShareChange, action: sharing.ActionKeyPolicyBind},
	"POST /self/keys":                                        {kind: writeSelf},
	"DELETE /self/keys/{key}":                                {kind: writeSelf},
	"DELETE /self/identities/{identity}":                     {kind: writeSelf},
	"DELETE /self/sessions/{session}":                        {kind: writeSelf},
	"PUT /self/keys/{key}/policy":                            {kind: writeOwnBinding, action: sharing.ActionKeyPolicyBind},
}

// Route keys the handlers name when they submit a change; each is an
// entry of registryWrites, and a handler whose change does not carry
// its entry's action is a programming error (submit refuses it).
const (
	routeCreateAudience = "POST /share/audiences"
	routePutMember      = "PUT /share/audiences/{audience}/members/{principal}"
	routeDeleteMember   = "DELETE /share/audiences/{audience}/members/{principal}"
	routeCreatePolicy   = "POST /share/policies"
	routeUpdatePolicy   = "PUT /share/policies/{policy}"
	routeInstallTpl     = "POST /share/templates/{name}"
	routeCreateGrant    = "POST /share/grants"
	routeRevokeGrant    = "DELETE /share/grants/{grant}"
	routeSetCeiling     = "PUT /principals/{id}/ceiling"
	routeBindKey        = "PUT /share/keys/{key}/policy"
	routeBindOwnKey     = "PUT /self/keys/{key}/policy"
)

// submit runs change c, which route (a registryWrites key) built, as the
// caller. It reports the written change and true, or has answered the
// request: 202 with the pending request, or the refusal.
func (s *shareRoutes) submit(w http.ResponseWriter, r *http.Request, route string, c sharing.Change) (*sharechange.Applied, bool) {
	class, ok := registryWrites[route]
	if !ok || class.action != c.Action() || (class.kind != writeShareChange && class.kind != writeOwnBinding) {
		slog.ErrorContext(r.Context(), "sharing route submits a change its class does not name", "route", route, "action", c.Action())
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not change the sharing registry")
		return nil, false
	}
	var (
		out *sharechange.Outcome
		err error
	)
	if class.kind == writeOwnBinding {
		out, err = s.svc.Direct(r.Context(), caller(r), c)
	} else {
		out, err = s.svc.Submit(r.Context(), caller(r), c)
	}
	if err != nil {
		writeChangeError(w, r, err)
		return nil, false
	}
	if out.Request != nil {
		w.Header().Set("Location", approvalLocation(out.Request.ID))
		WriteJSON(w, http.StatusAccepted, recordOf(out.Request, approval.StateRequested, false))
		return nil, false
	}
	return out.Applied, true
}

// writeChangeError answers a change that failed: a write that the
// registry refused with the route's own message, a filing the mechanism
// refused with its code, anything else a 500 that names no cause.
func writeChangeError(w http.ResponseWriter, r *http.Request, err error) {
	var ce *sharechange.Error
	var ae *approval.Error
	switch {
	case errors.As(err, &ce):
		writeRegistryError(w, ce.Err, ce.What)
	case errors.As(err, &ae):
		writeApprovalError(w, r, err)
	case errors.Is(err, sharechange.ErrNoApprovals):
		WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED",
			"this instance has no approval mechanism: only the owner can make a change that widens access")
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, storage.ErrAlreadyExists):
		writeRegistryError(w, err, "sharing change")
	default:
		slog.ErrorContext(r.Context(), "sharing change failed", "route", r.Method+" "+r.URL.Path, "err", err)
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not change the sharing registry")
	}
}
