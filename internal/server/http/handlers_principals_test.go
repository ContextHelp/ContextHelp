package http

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

type listOf[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

// keyOf returns the id of principal's first key.
func (f *uiFixture) keyOf(principal string) string {
	f.t.Helper()
	keys, err := f.keys.ListByPrincipal(ownerCtx(), principal)
	require.NoError(f.t, err)
	require.NotEmpty(f.t, keys)
	return keys[0].ID
}

func (f *uiFixture) createPrincipal(tok, kind, name string, roles ...string) *http.Response {
	f.t.Helper()
	body := map[string]any{"kind": kind, "display_name": name}
	if len(roles) > 0 {
		body["roles"] = roles
	}
	return f.do(http.MethodPost, "/api/v1/principals", body, withBearer(tok))
}

func TestSelf_PersonManagesOwnKeysAndSessions(t *testing.T) {
	f := newCeremonyFixture(t)
	cookie, signed := f.acceptInvite("Bob")
	me := signed.Principal

	// Create a key through the session; it authenticates as the person
	// with the session's scopes, never the full role bundle.
	resp := f.do(http.MethodPost, "/api/v1/self/keys", map[string]any{"name": "laptop"}, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	created := decodeBody[createdKey](t, resp)
	assert.Equal(t, me, created.PrincipalID)
	assert.Equal(t, "laptop", created.Name)
	assert.NotContains(t, created.Scopes, authn.ScopeReadUI, "the session never held read:ui, so neither does its key")
	assert.NotContains(t, created.Scopes, authn.ScopeSignoutUI)
	assert.Contains(t, created.Scopes, authn.ScopeReadObjects)

	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(created.Key))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	who := decodeBody[whoamiResponse](t, resp)
	assert.Equal(t, me, who.Principal)
	assert.Equal(t, "token", who.Via)

	resp = f.do(http.MethodGet, "/api/v1/self/keys", nil, withBearer(created.Key))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	keys := decodeBody[listOf[authn.KeyInfo]](t, resp)
	require.Len(t, keys.Data, 1)
	assert.Equal(t, created.ID, keys.Data[0].ID)
	assert.NotNil(t, keys.Data[0].LastUsedAt)

	// The key asks for more than its roles grant, or more than it
	// holds: refused either way.
	resp = f.do(http.MethodPost, "/api/v1/self/keys", map[string]any{"scopes": []string{string(authn.ScopeAdminPrincipals)}}, withBearer(created.Key))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, CodeInvalidScope, errCode(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/self/keys", map[string]any{"scopes": []string{string(authn.ScopeReadMCP)}}, withBearer(created.Key))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "read:mcp is a reader scope the session never held, so its key lacks it")
	assert.Equal(t, CodeScopeNotHeld, errCode(t, resp))

	// Revoke the key: it stops authenticating at once.
	resp = f.do(http.MethodDelete, "/api/v1/self/keys/"+created.ID, nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(created.Key))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/self/keys", nil, withCookie(cookie), fromUI)
	keys = decodeBody[listOf[authn.KeyInfo]](t, resp)
	require.Len(t, keys.Data, 1)
	assert.NotNil(t, keys.Data[0].RevokedAt)

	// Identities: the invite's link row is listed; sessions: the
	// current one, marked.
	resp = f.do(http.MethodGet, "/api/v1/self/identities", nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	idns := decodeBody[listOf[storage.Identity]](t, resp)
	var methods []string
	for _, i := range idns.Data {
		methods = append(methods, i.Method)
	}
	assert.ElementsMatch(t, []string{storage.IdentityMethodLink, storage.IdentityMethodAPIKey}, methods)

	resp = f.do(http.MethodGet, "/api/v1/self/sessions", nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	sessions := decodeBody[listOf[ownSession]](t, resp)
	require.Len(t, sessions.Data, 1)
	assert.True(t, sessions.Data[0].Current)
	assert.Equal(t, "active", sessions.Data[0].Status)
	assert.Equal(t, signed.Session.ID, sessions.Data[0].ID)

	// End the session: the cookie is cleared and refused afterwards.
	resp = f.do(http.MethodDelete, "/api/v1/self/sessions/"+signed.Session.ID, nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, -1, sessionCookieOf(t, resp).MaxAge, "cookie cleared")
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSelf_RefusesAnotherPrincipalsCredentials(t *testing.T) {
	f := newCeremonyFixture(t)
	owner := f.signIn()
	ownerWho := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(owner), fromUI))
	cookie, _ := f.acceptInvite("Bob")

	resp := f.do(http.MethodDelete, "/api/v1/self/keys/"+f.keyOf(storage.OwnerPrincipal), nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeNotSelf, errCode(t, resp))

	resp = f.do(http.MethodDelete, "/api/v1/self/sessions/"+ownerWho.Session.ID, nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeNotSelf, errCode(t, resp))

	ownerIdns, err := f.bundle.svc.Store(ownerCtx()).Auth().Identities().ListByPrincipal(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(t, err)
	resp = f.do(http.MethodDelete, "/api/v1/self/identities/"+ownerIdns[0].ID, nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeNotSelf, errCode(t, resp))

	// Nothing changed: the owner's key, session and identity still work.
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(owner), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The person's own listing shows only the person's rows.
	resp = f.do(http.MethodGet, "/api/v1/self/keys", nil, withCookie(cookie), fromUI)
	assert.Equal(t, 0, decodeBody[listOf[authn.KeyInfo]](t, resp).Total)
}

func TestAdmin_RoutesRefuseReader(t *testing.T) {
	f := newCeremonyFixture(t)
	cookie, who := f.acceptInvite("Bob")
	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/principals"},
		{http.MethodPost, "/api/v1/principals"},
		{http.MethodGet, "/api/v1/principals/owner"},
		{http.MethodPost, "/api/v1/principals/owner/suspend"},
		{http.MethodPost, "/api/v1/principals/owner/revoke"},
		{http.MethodPost, "/api/v1/principals/owner/links"},
		{http.MethodGet, "/api/v1/principals/owner/identities"},
		{http.MethodPost, "/api/v1/principals/owner/identities"},
		{http.MethodGet, "/api/v1/principals/owner/keys"},
		{http.MethodPost, "/api/v1/principals/owner/keys"},
		{http.MethodPost, "/api/v1/share/invites"},
	} {
		// The reader's key and the reader's session alike.
		resp := f.do(call.method, call.path, nil, withBearer(f.other))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s by key", call.method, call.path)
		assert.Equal(t, CodeInsufficientScope, errCode(t, resp))
		resp = f.do(call.method, call.path, nil, withCookie(cookie), fromUI)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s by %s's session", call.method, call.path, who.Principal)
	}
}

func TestAdmin_PrincipalLifecycle(t *testing.T) {
	f := newCeremonyFixture(t)

	resp := f.createPrincipal(f.ops, storage.PrincipalKindDevice, "Phone")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	device := decodeBody[storage.Principal](t, resp)
	assert.Equal(t, []string{authn.RoleWriter}, device.Roles, "a device captures by default")
	assert.Equal(t, storage.PrincipalActive, device.Status)

	resp = f.createPrincipal(f.ops, storage.PrincipalKindPerson, "Dana", authn.RoleAdmin)
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, []string{authn.RoleAdmin}, decodeBody[storage.Principal](t, resp).Roles)

	for _, bad := range []map[string]any{
		{"kind": "owner", "display_name": "x"},
		{"kind": "robot", "display_name": "x"},
		{"kind": "person", "display_name": " "},
		{"kind": "person", "display_name": "x", "roles": []string{"root"}},
	} {
		resp = f.do(http.MethodPost, "/api/v1/principals", bad, withBearer(f.ops))
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "%v", bad)
	}

	resp = f.do(http.MethodGet, "/api/v1/principals?kind=device", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	list := decodeBody[listOf[storage.Principal]](t, resp)
	require.Equal(t, 1, list.Total)
	assert.Equal(t, device.ID, list.Data[0].ID)
	resp = f.do(http.MethodGet, "/api/v1/principals?kind=robot", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = f.do(http.MethodGet, "/api/v1/principals/"+device.ID, nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "Phone", decodeBody[storage.Principal](t, resp).DisplayName)
	resp = f.do(http.MethodGet, "/api/v1/principals/prn_nobody", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// An admin mints the device its key; the key acts as the device.
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/keys", map[string]any{"name": "capture", "expires_in": 3600}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	key := decodeBody[createdKey](t, resp)
	assert.False(t, key.ExpiresAt.IsZero())
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(key.Key))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, device.ID, decodeBody[whoamiResponse](t, resp).Principal)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/keys", map[string]any{"expires_in": 60, "expires_at": "2030-01-01T00:00:00Z"}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "two expiries")

	resp = f.do(http.MethodGet, "/api/v1/principals/"+device.ID+"/keys", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 1, decodeBody[listOf[authn.KeyInfo]](t, resp).Total)

	// Bind a proxy login by hand; keys cannot be bound that way.
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/identities",
		map[string]any{"method": storage.IdentityMethodProxy, "issuer": "tailscale", "subject": "phone@example.com"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	proxy := decodeBody[storage.Identity](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/identities",
		map[string]any{"method": storage.IdentityMethodAPIKey, "subject": "x"}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/principals/"+device.ID+"/identities", nil, withBearer(f.ops))
	assert.Equal(t, 2, decodeBody[listOf[storage.Identity]](t, resp).Total, "the key's row and the proxy login")
	resp = f.do(http.MethodDelete, "/api/v1/principals/"+device.ID+"/identities/"+proxy.ID, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.do(http.MethodDelete, "/api/v1/principals/owner/identities/"+proxy.ID, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "an identity is addressed under its own principal")

	// Suspend: the key is refused; revoke: everything is gone.
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/suspend", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, storage.PrincipalSuspended, decodeBody[storage.Principal](t, resp).Status)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(key.Key))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/keys", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodePrincipalInactive, errCode(t, resp))

	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/revoke", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, storage.PrincipalRevoked, decodeBody[storage.Principal](t, resp).Status)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/suspend", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	// The owner is protected.
	resp = f.do(http.MethodPost, "/api/v1/principals/owner/revoke", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeOwnerProtected, errCode(t, resp))

	// Admin key revoke on another principal's key, addressed under it.
	otherKey := f.keyOf("other")
	resp = f.do(http.MethodDelete, "/api/v1/principals/owner/keys/"+otherKey, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	resp = f.do(http.MethodDelete, "/api/v1/principals/other/keys/"+otherKey, nil, withBearer(f.ops))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.other))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAdmin_RevokingLinkIdentityEndsPersonsSession(t *testing.T) {
	f := newCeremonyFixture(t)
	cookie, who := f.acceptInvite("Bob")

	resp := f.do(http.MethodGet, "/api/v1/principals/"+who.Principal+"/identities", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	idns := decodeBody[listOf[storage.Identity]](t, resp)
	require.Len(t, idns.Data, 1)
	assert.Equal(t, storage.IdentityMethodLink, idns.Data[0].Method)

	resp = f.do(http.MethodDelete, "/api/v1/principals/"+who.Principal+"/identities/"+idns.Data[0].ID, nil, withBearer(f.ops))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(cookie), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// The person still exists and can be sent a new link.
	resp = f.signInLink(f.ops, who.Principal)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestPeer_KeyAuthenticatesFederationPush(t *testing.T) {
	f := newUIFixture(t)
	f.requirePeer = true
	f.startCeremonies(config.AuthSignInInvite, config.AuthSignInLink)

	resp := f.createPrincipal(f.ops, storage.PrincipalKindPeer, "Partner instance")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	peer := decodeBody[storage.Principal](t, resp)
	assert.Equal(t, []string{authn.RoleWriter}, peer.Roles, "a peer pushes by default")
	resp = f.do(http.MethodPost, "/api/v1/principals/"+peer.ID+"/keys", map[string]any{"name": "push"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	key := decodeBody[createdKey](t, resp)

	push := func(tok string) *http.Response {
		body := makePushBody(t, []storage.KnowledgeObject{makeKO("fed-1", "h1")}, nil)
		req, err := http.NewRequest(http.MethodPost, f.ts.URL+"/api/v1/federation/push", body)
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	resp = push(f.ops)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "PEER_REQUIRED", errCode(t, resp))
	resp = push(key.Key)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, 1, decodeBody[map[string]int](t, resp)["accepted"])
}
