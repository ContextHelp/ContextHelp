package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// An administrator refused a credential for another principal leaves a
// denied receipt whose route_access check fails with the stable reason
// credential_owner_only; the same administrator minting its own key is
// allowed, and the owner is not recorded.
func TestReceiptCredentialDeliveryRefusal(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guardCredentialDelivery(w, r, "prn_target") {
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{})
	})
	invite := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guardInviteDelivery(w, r) {
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{})
	})
	rig := newReceiptRig(t, false,
		apiRoute{Method: http.MethodPost, Pattern: "/principals/{id}/keys", Scope: authn.ScopeAdminPrincipals, Handler: ok},
		apiRoute{Method: http.MethodPost, Pattern: "/share/invites", Scope: authn.ScopeAdminShares, Handler: invite},
	)
	admin := &authn.Principal{
		ID: "prn_admin", Provider: authn.MethodAPIKey, Kind: storage.PrincipalKindPerson,
		Scopes: []authn.Scope{authn.ScopeAdminPrincipals, authn.ScopeAdminShares},
		Meta:   map[string]string{authn.MetaIdentityID: "idn_admin"},
	}
	v := restrictedViewer(t)

	require.Equal(t, http.StatusForbidden, rig.do(v, admin, http.MethodPost, "/principals/prn_target/keys").Code)
	require.Equal(t, http.StatusForbidden, rig.do(v, admin, http.MethodPost, "/share/invites").Code)

	es := rig.entries()
	require.Len(t, es, 2)
	for _, e := range es {
		assert.Equal(t, storage.AccessDecisionDenied, e.Decision, e.Route)
		assert.Contains(t, checkTrail(e), "route_access=FAIL:"+sharing.ReasonCredentialOwnerOnly, e.Route)
	}
	assert.Equal(t, "credential_owner_only", sharing.ReasonCredentialOwnerOnly)
}

// An administrator acting on the owner's credentials is refused by the
// owner guard, and the refusal is a denied receipt too: route_access
// FAIL owner_protected.
func TestReceiptOwnerGuardRefusal(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ownerGuard(w, r, storage.OwnerPrincipal) {
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{})
	})
	rig := newReceiptRig(t, false,
		apiRoute{Method: http.MethodGet, Pattern: "/principals/{id}/keys", Scope: authn.ScopeAdminPrincipals, Handler: h},
	)
	admin := &authn.Principal{
		ID: "prn_admin", Provider: authn.MethodAPIKey, Kind: storage.PrincipalKindPerson,
		Scopes: []authn.Scope{authn.ScopeAdminPrincipals},
		Meta:   map[string]string{authn.MetaIdentityID: "idn_admin"},
	}
	require.Equal(t, http.StatusForbidden, rig.do(restrictedViewer(t), admin, http.MethodGet, "/principals/owner/keys").Code)
	es := rig.entries()
	require.Len(t, es, 1)
	assert.Equal(t, storage.AccessDecisionDenied, es[0].Decision)
	assert.Contains(t, checkTrail(es[0]), "route_access=FAIL:"+sharing.ReasonOwnerProtected)
}
