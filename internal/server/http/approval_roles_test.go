package http

import (
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ADR-082 §1: the approval roles are outside admin, so an admin, which
// cannot hold their scopes, cannot hand them out either (no route
// yields more than the caller holds). Only the owner assigns them.
func TestApprovalRoles_OnlyTheOwnerAssignsThem(t *testing.T) {
	f := newCeremonyFixture(t)
	_, adminKey := f.adminKey("Key admin")
	for _, role := range authn.ApprovalRoles {
		t.Run(role, func(t *testing.T) {
			resp := f.createPrincipal(adminKey, storage.PrincipalKindPerson, "by admin "+role, authn.RoleReader, role)
			require.Equal(t, http.StatusForbidden, resp.StatusCode)
			assert.Equal(t, CodeScopeNotHeld, errCode(t, resp))

			resp = f.createPrincipal(f.ops, storage.PrincipalKindPerson, "by owner "+role, authn.RoleReader, role)
			require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
			p := decodeBody[storage.Principal](t, resp)
			assert.Equal(t, []string{authn.RoleReader, role}, p.Roles)
		})
	}
}

// The registry's owner row carries authn.OwnerRoles (migration 060 /
// Postgres 41), and its key holds every approval scope; an admin's key
// holds none.
func TestApprovalRoles_OwnerHoldsThemAdminDoesNot(t *testing.T) {
	f := newCeremonyFixture(t)
	owner, err := f.bundle.svc.Store(ownerCtx()).Auth().Principals().Get(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(t, err)
	assert.Equal(t, authn.OwnerRoles, owner.Roles)

	_, adminKey := f.adminKey("Key admin")
	ownerWho := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.ops)))
	adminWho := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(adminKey)))
	for _, s := range []authn.Scope{authn.ScopeRequestSteward, authn.ScopeApproveSteward, authn.ScopeApproveShares} {
		assert.True(t, slices.Contains(ownerWho.Scopes, s), "the owner lacks %s", s)
		assert.False(t, slices.Contains(adminWho.Scopes, s), "an admin holds %s", s)
	}
}
