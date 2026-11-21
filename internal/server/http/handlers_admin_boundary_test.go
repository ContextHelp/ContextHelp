package http

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Instance admin is not owner (ADR-079 §5, ADR-082 §10, ADR-083 §10): a principal
// holding the admin bundle administers principals and shares, and the
// owner stays out of its reach. The tests here call the registry as the
// two kinds of administrator an instance has: a device presenting a
// key with the whole bundle, and a person signed in to the web UI,
// whose session carries the UI scope set intersected with the bundle.

// ownerState is the owner's credential state: what an administrator's
// attempts must leave as they found it.
type ownerState struct {
	keys       []string
	identities []string
	status     string
	owners     int
}

func (f *uiFixture) ownerState() ownerState {
	f.t.Helper()
	auth := f.bundle.svc.Store(ownerCtx()).Auth()
	var s ownerState
	keys, err := f.keys.ListByPrincipal(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(f.t, err)
	for _, k := range keys {
		s.keys = append(s.keys, k.ID+"/"+boolWord(k.RevokedAt != nil))
	}
	idns, err := auth.Identities().ListByPrincipal(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(f.t, err)
	for _, i := range idns {
		s.identities = append(s.identities, i.ID+"/"+boolWord(!i.Active()))
	}
	owner, err := auth.Principals().Get(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(f.t, err)
	s.status = owner.Status
	all, err := auth.Principals().List(ownerCtx(), storage.PrincipalFilter{Kind: storage.PrincipalKindOwner})
	require.NoError(f.t, err)
	s.owners = len(all)
	return s
}

func boolWord(b bool) string {
	if b {
		return "gone"
	}
	return "live"
}

// An administrator cannot mint, rotate, revoke or bind the owner's
// credentials, suspend or revoke the owner, bind a login to it, end its
// sessions or narrow its keys, by any route and under either
// credential. Each refusal names the layer that made it (the scope,
// the owner guard, the self check) and leaves the owner's keys,
// identities and status as they were.
func TestAdminBoundary_OwnerCredentialsAreUntouchable(t *testing.T) {
	f := newCeremonyFixture(t)
	ownerCookie := f.signIn()
	ownerWho := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(ownerCookie), fromUI))
	ownerKey := f.keyOf(storage.OwnerPrincipal)
	ownerIdns, err := f.bundle.svc.Store(ownerCtx()).Auth().Identities().ListByPrincipal(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(t, err)
	var ownerKeyIdentity string
	for _, i := range ownerIdns {
		if i.Method == storage.IdentityMethodAPIKey {
			ownerKeyIdentity = i.ID
		}
	}
	require.NotEmpty(t, ownerKeyIdentity)
	require.NotEmpty(t, ownerWho.Session.ID)
	before := f.ownerState()

	calls := []struct {
		method, path string
		body         any
		scope        authn.Scope
		// code is the refusal when the credential holds scope.
		code string
	}{
		{http.MethodPost, "/api/v1/principals/owner/keys", map[string]any{"name": "backdoor"}, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPost, "/api/v1/principals/owner/links", nil, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPost, "/api/v1/principals/owner/suspend", nil, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPost, "/api/v1/principals/owner/revoke", nil, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPost, "/api/v1/principals/owner/identities", map[string]any{"method": "proxy", "issuer": "tailscale", "subject": "mallory@example.com"}, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPost, "/api/v1/principals/owner/identities", map[string]any{"method": "oidc", "issuer": "https://idp.example", "subject": "mallory"}, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodDelete, "/api/v1/principals/owner/identities/" + ownerKeyIdentity, nil, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodDelete, "/api/v1/principals/owner/keys/" + ownerKey, nil, authn.ScopeAdminPrincipals, CodeOwnerProtected},
		{http.MethodPut, "/api/v1/share/keys/" + ownerKey + "/policy", map[string]any{"policy": ""}, authn.ScopeAdminShares, CodeOwnerProtected},
		{http.MethodPut, "/api/v1/self/keys/" + ownerKey + "/policy", map[string]any{"policy": ""}, authn.ScopeWriteSelf, CodeNotSelf},
		{http.MethodDelete, "/api/v1/self/keys/" + ownerKey, nil, authn.ScopeWriteSelf, CodeNotSelf},
		{http.MethodDelete, "/api/v1/self/identities/" + ownerKeyIdentity, nil, authn.ScopeWriteSelf, CodeNotSelf},
		{http.MethodDelete, "/api/v1/self/sessions/" + ownerWho.Session.ID, nil, authn.ScopeWriteSelf, CodeNotSelf},
	}
	for _, c := range f.adminCallers() {
		for _, call := range calls {
			resp := f.callAs(c, call.method, call.path, call.body)
			want := call.code
			if !c.holds(call.scope) {
				want = CodeInsufficientScope
			}
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s admin: %s %s", c.name, call.method, call.path)
			assert.Equal(t, want, errCode(t, resp), "%s admin: %s %s", c.name, call.method, call.path)
		}
	}
	assert.Equal(t, before, f.ownerState(), "the owner's keys, identities and status are untouched")
	// The owner's key and its browser session still work.
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.ops)).StatusCode)
	assert.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(ownerCookie), fromUI).StatusCode)
}

// What an administrator may read of the owner's credentials is their
// metadata: ids, names, scopes and times. The key secret, the stored
// hash and the owner's key itself are in no answer.
func TestAdminBoundary_OwnerCredentialListsHoldNoSecret(t *testing.T) {
	f := newCeremonyFixture(t)
	ownerKey := f.keyOf(storage.OwnerPrincipal)
	admin := f.adminCallers()[0] // the key administrator: the session one lacks admin:principals
	for _, path := range []string{"/api/v1/principals/owner/keys", "/api/v1/principals/owner/identities", "/api/v1/principals/owner"} {
		resp := f.callAs(admin, http.MethodGet, path, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		body := rawBody(t, resp)
		assert.NotContains(t, body, f.ops, "%s carries the owner's key", path)
		assert.NotContains(t, strings.ToLower(body), "hash", path)
		assert.NotContains(t, strings.ToLower(body), "secret", path)
	}
	require.NotEmpty(t, ownerKey)
}

// There is exactly one owner and no route makes a second. Creating a
// principal as the owner kind, with the owner's id or with a role
// beyond the bundles is refused; an administrator mints no invite, and
// the owner's invite redeems to a person holding the reader role
// whatever its body asks for; and the owner joins no audience.
func TestAdminBoundary_NoRouteCreatesAnOwner(t *testing.T) {
	f := newCeremonyFixture(t)
	for _, c := range f.adminCallers() {
		if c.holds(authn.ScopeAdminPrincipals) {
			for _, body := range []map[string]any{
				{"kind": "owner", "display_name": "Mallory"},
				{"kind": "owner", "display_name": "Mallory", "roles": []string{"admin"}},
				{"kind": "person", "display_name": "Mallory", "roles": []string{"owner"}},
				{"kind": "person", "id": "owner", "display_name": "Mallory"},
			} {
				resp := f.callAs(c, http.MethodPost, "/api/v1/principals", body)
				if body["id"] == "owner" {
					// The id is the registry's to assign: the body's is ignored.
					require.Equal(t, http.StatusCreated, resp.StatusCode, "%s admin: %v", c.name, body)
					got := decodeBody[storage.Principal](t, resp)
					assert.NotEqual(t, storage.OwnerPrincipal, got.ID, "%s admin", c.name)
					assert.Equal(t, storage.PrincipalKindPerson, got.Kind)
					continue
				}
				assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s admin: %v", c.name, body)
				assert.Equal(t, "INVALID_REQUEST", errCode(t, resp), "%s admin: %v", c.name, body)
			}
		}
		// An administrator mints no invite: only the owner can.
		body := map[string]any{"display_name": "Mallory " + c.name, "kind": "owner", "roles": []string{"admin"}}
		resp := f.callAs(c, http.MethodPost, "/api/v1/share/invites", body)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s admin", c.name)
		assert.Equal(t, CodeCredentialForOtherPrincipal, errCode(t, resp), "%s admin", c.name)
		// The owner's invite, with the same body, redeems to a reader person.
		resp = f.do(http.MethodPost, "/api/v1/share/invites", body, withBearer(f.ops))
		require.Equal(t, http.StatusCreated, resp.StatusCode, "%s: %s", c.name, errBody(t, resp))
		redeemed := f.redeemInvite(secretOf(t, decodeBody[linkResponse](t, resp).URL))
		require.Equal(t, http.StatusOK, redeemed.StatusCode, errBody(t, redeemed))
		who := decodeBody[exchangeResponse](t, redeemed)
		p, err := f.bundle.svc.Store(ownerCtx()).Auth().Principals().Get(ownerCtx(), who.Principal)
		require.NoError(t, err)
		assert.Equal(t, storage.PrincipalKindPerson, p.Kind, "%s admin's invitee", c.name)
		assert.Equal(t, []string{authn.RoleReader}, p.Roles, "%s admin's invitee", c.name)

		// The owner sees everything and joins no audience.
		resp = f.callAs(c, http.MethodPost, "/api/v1/share/audiences", map[string]any{"name": "aud-" + c.name})
		require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
		resp = f.callAs(c, http.MethodPut, "/api/v1/share/audiences/aud-"+c.name+"/members/owner", map[string]any{})
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%s admin", c.name)
	}
	assert.Equal(t, 1, f.ownerState().owners)
}

// An administrator setting the owner's class ceiling changes nothing
// the owner sees: the owner's viewer is the unrestricted one and no
// stored ceiling narrows it.
func TestAdminBoundary_OwnerViewIgnoresCeiling(t *testing.T) {
	f := newCeremonyFixture(t)
	seedScopeObject(t, f.bundle)
	list := func() int {
		resp := f.do(http.MethodGet, "/api/v1/objects", nil, withBearer(f.ops))
		require.Equal(t, http.StatusOK, resp.StatusCode)
		return decodeBody[listOf[storage.KnowledgeObject]](t, resp).Total
	}
	require.Positive(t, list())
	owned := list()
	for _, c := range f.adminCallers() {
		resp := f.callAs(c, http.MethodPut, "/api/v1/principals/owner/ceiling", map[string]any{"deny_classes": []string{"personal", "financial"}})
		t.Logf("%s admin sets the owner's ceiling: %d", c.name, resp.StatusCode)
		assert.Equal(t, owned, list(), "%s admin: the owner's listing", c.name)
	}
}

// rawBody reads a response body as text.
func rawBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(b)
}
