package http

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// adminKey creates a person with the admin role and mints it a key
// narrowed to scopes (nil = the whole admin bundle), as the owner.
func (f *uiFixture) adminKey(name string, scopes ...authn.Scope) (id, key string) {
	f.t.Helper()
	resp := f.createPrincipal(f.ops, storage.PrincipalKindPerson, name, authn.RoleAdmin)
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	p := decodeBody[storage.Principal](f.t, resp)
	body := map[string]any{"name": name}
	if len(scopes) > 0 {
		body["scopes"] = scopes
	}
	resp = f.do(http.MethodPost, "/api/v1/principals/"+p.ID+"/keys", body, withBearer(f.ops))
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	return p.ID, decodeBody[createdKey](f.t, resp).Key
}

// The owner is protected: another admin cannot mint the owner a key or
// a sign-in link, suspend or revoke it, or touch its identities and
// keys; the owner itself can, and the host command stays the
// bootstrap.
func TestOwnerGuard_OtherAdminsNeverTouchTheOwner(t *testing.T) {
	f := newCeremonyFixture(t)
	_, dana := f.adminKey("Dana")
	ownerIdns, err := f.bundle.svc.Store(ownerCtx()).Auth().Identities().ListByPrincipal(ownerCtx(), storage.OwnerPrincipal)
	require.NoError(t, err)

	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/principals/owner/keys", map[string]any{"name": "backdoor"}},
		{http.MethodPost, "/api/v1/principals/owner/links", nil},
		{http.MethodPost, "/api/v1/principals/owner/suspend", nil},
		{http.MethodPost, "/api/v1/principals/owner/revoke", nil},
		{http.MethodPost, "/api/v1/principals/owner/identities", map[string]any{"method": "proxy", "issuer": "tailscale", "subject": "dana@example.com"}},
		{http.MethodDelete, "/api/v1/principals/owner/identities/" + ownerIdns[0].ID, nil},
		{http.MethodDelete, "/api/v1/principals/owner/keys/" + f.keyOf(storage.OwnerPrincipal), nil},
	} {
		resp := f.do(call.method, call.path, call.body, withBearer(dana))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s as another admin", call.method, call.path)
		assert.Equal(t, CodeOwnerProtected, errCode(t, resp), "%s %s", call.method, call.path)
	}
	// Nothing changed: the owner's key still works, no key was added.
	resp := f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/principals/owner/keys", nil, withBearer(dana))
	require.Equal(t, http.StatusOK, resp.StatusCode, "reads stay open to admins")
	assert.Equal(t, 1, decodeBody[listOf[authn.KeyInfo]](t, resp).Total)

	// The owner manages its own credentials through the same routes.
	resp = f.do(http.MethodPost, "/api/v1/principals/owner/keys", map[string]any{"name": "second laptop"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, storage.OwnerPrincipal, decodeBody[createdKey](t, resp).PrincipalID)
	resp = f.do(http.MethodPost, "/api/v1/principals/owner/links", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/principals/owner/identities",
		map[string]any{"method": "proxy", "issuer": "tailscale", "subject": "owner@example.com"}, withBearer(f.ops))
	assert.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	// Suspending or revoking the owner is refused by the registry
	// itself, whoever asks.
	resp = f.do(http.MethodPost, "/api/v1/principals/owner/suspend", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, CodeOwnerProtected, errCode(t, resp))
}

// Keys narrow, never widen, for an admin too: a key holds at most the
// principal's role scopes and at most what the minting key holds. An
// admin mints keys for itself only (only the owner issues a credential
// for another principal), so its ceiling is tested on its own keys,
// through the admin route and /self alike.
func TestKeyCeiling_AdminNeverMintsMoreThanItHolds(t *testing.T) {
	f := newCeremonyFixture(t)
	// An admin whose own key is narrowed to reader-level reads plus the
	// scope that reaches the registry.
	held := []authn.Scope{authn.ScopeAdminPrincipals, authn.ScopeReadObjects, authn.ScopeReadSystem, authn.ScopeWriteSelf}
	narrowID, narrow := f.adminKey("Narrow", held...)
	resp := f.createPrincipal(f.ops, storage.PrincipalKindDevice, "Phone")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	device := decodeBody[storage.Principal](t, resp)
	keysPath := "/api/v1/principals/" + device.ID + "/keys"
	ownPath := "/api/v1/principals/" + narrowID + "/keys"

	// Another principal's key is the owner's to mint, whatever is asked.
	for _, body := range []map[string]any{
		{"name": "default"},
		{"scopes": []authn.Scope{authn.ScopeWriteObjects}},
		{"scopes": []authn.Scope{authn.ScopeReadObjects}},
	} {
		resp = f.do(http.MethodPost, keysPath, body, withBearer(narrow))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%v", body)
		assert.Equal(t, CodeCredentialForOtherPrincipal, errCode(t, resp), "%v", body)
	}

	// Its own key through the admin route is the /self form: without
	// scopes it holds what the narrowed key holds, never the bundle.
	resp = f.do(http.MethodPost, ownPath, map[string]any{"name": "default"}, withBearer(narrow))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.ElementsMatch(t, held, decodeBody[createdKey](t, resp).Scopes)

	// A scope its roles grant but its key lacks, on either route.
	for _, path := range []string{ownPath, "/api/v1/self/keys"} {
		for _, s := range []authn.Scope{authn.ScopeWriteObjects, authn.ScopeReadJobs} {
			resp = f.do(http.MethodPost, path, map[string]any{"scopes": []authn.Scope{s}}, withBearer(narrow))
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s %s", path, s)
			assert.Equal(t, CodeScopeNotHeld, errCode(t, resp), "%s %s", path, s)
		}
	}

	// A scope the device's roles do not grant at all is a bad request,
	// whoever asks.
	resp = f.do(http.MethodPost, keysPath, map[string]any{"scopes": []authn.Scope{authn.ScopeAdminAudit}}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, CodeInvalidScope, errCode(t, resp))
	resp = f.do(http.MethodPost, keysPath, map[string]any{"scopes": []authn.Scope{"read:everything"}}, withBearer(f.ops))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Equal(t, CodeInvalidScope, errCode(t, resp))

	// What it holds goes through, and the key holds exactly that.
	asked := []authn.Scope{authn.ScopeReadObjects, authn.ScopeReadSystem}
	resp = f.do(http.MethodPost, ownPath, map[string]any{"scopes": asked}, withBearer(narrow))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	created := decodeBody[createdKey](t, resp)
	assert.Equal(t, asked, created.Scopes)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(created.Key))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, asked, decodeBody[whoamiResponse](t, resp).Scopes)
	// /self without scopes: the key's own scopes, never the bundle.
	resp = f.do(http.MethodPost, "/api/v1/self/keys", map[string]any{"name": "same"}, withBearer(narrow))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.ElementsMatch(t, held, decodeBody[createdKey](t, resp).Scopes)

	// The owner holds everything: the default bundle goes through.
	resp = f.do(http.MethodPost, keysPath, map[string]any{"name": "full"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, authn.ScopesForRoles([]string{authn.RoleWriter}), decodeBody[createdKey](t, resp).Scopes)

	// A browser session's key is the session's reads, never the role.
	cookie, _ := f.acceptInvite("Bob")
	resp = f.do(http.MethodPost, "/api/v1/self/keys", nil, withCookie(cookie), fromUI)
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	sessionKey := decodeBody[createdKey](t, resp)
	assert.NotContains(t, sessionKey.Scopes, authn.ScopeReadUI)
	assert.NotContains(t, sessionKey.Scopes, authn.ScopeReadMCP)
	assert.Subset(t, authn.ScopesForRoles([]string{authn.RoleReader}), sessionKey.Scopes)
}

// Every credential-producing route sits under one ceiling: a principal
// created, an identity bound, a sign-in link or a key minted never
// holds more than the caller. On top of it, an identity, a link or a
// key for another principal is the owner's alone: an admin, full key
// or narrowed, is refused 403 CREDENTIAL_FOR_OTHER_PRINCIPAL whatever
// it holds. Callers: the owner, an admin with a full key, an admin
// with a reader-level key, a reader. Targets: a writer-level device
// and a reader-level person.
func TestCeiling_EveryCredentialPath(t *testing.T) {
	f := newCeremonyFixture(t)
	_, full := f.adminKey("Full")
	_, narrow := f.adminKey("Narrow", authn.ScopeAdminPrincipals, authn.ScopeAdminShares,
		authn.ScopeReadObjects, authn.ScopeReadSystem, authn.ScopeReadJobs, authn.ScopeReadInbox,
		authn.ScopeReadFeeds, authn.ScopeReadRegistries, authn.ScopeReadMCP, authn.ScopeReadPipelines,
		authn.ScopeReadWatches, authn.ScopeReadUI, authn.ScopeWriteSelf)

	newTarget := func(kind, name string) storage.Principal {
		resp := f.createPrincipal(f.ops, kind, name)
		require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
		return decodeBody[storage.Principal](t, resp)
	}
	// Fresh targets per caller, so a refusal is never explained by an
	// earlier call's state.
	type target struct {
		roles []string
		kind  string
	}
	writer := target{roles: []string{authn.RoleWriter}, kind: storage.PrincipalKindDevice}
	reader := target{roles: []string{authn.RoleReader}, kind: storage.PrincipalKindPerson}

	callers := []struct {
		name string
		key  string
		// want is the status for a writer-level and a reader-level
		// target; code the error code on a refusal.
		wantWriter, wantReader int
		code                   string
	}{
		{"owner", f.ops, http.StatusCreated, http.StatusCreated, ""},
		{"admin person, full key", full, http.StatusCreated, http.StatusCreated, ""},
		{"admin person, reader-level key", narrow, http.StatusForbidden, http.StatusCreated, CodeScopeNotHeld},
		{"reader person", f.other, http.StatusForbidden, http.StatusForbidden, CodeInsufficientScope},
	}
	n := 0
	for _, c := range callers {
		for _, tg := range []struct {
			name string
			target
			want int
		}{{"writer", writer, c.wantWriter}, {"reader", reader, c.wantReader}} {
			n++
			p := newTarget(tg.kind, fmt.Sprintf("%s %d", tg.name, n))
			actions := []struct {
				name string
				call func() *http.Response
			}{
				{"create", func() *http.Response {
					return f.do(http.MethodPost, "/api/v1/principals",
						map[string]any{"kind": tg.kind, "display_name": fmt.Sprintf("new %d", n), "roles": tg.roles}, withBearer(c.key))
				}},
				{"identity add", func() *http.Response {
					return f.do(http.MethodPost, "/api/v1/principals/"+p.ID+"/identities",
						map[string]any{"method": "proxy", "issuer": "tailscale", "subject": fmt.Sprintf("login-%d@example.com", n)}, withBearer(c.key))
				}},
				{"link mint", func() *http.Response {
					return f.do(http.MethodPost, "/api/v1/principals/"+p.ID+"/links", nil, withBearer(c.key))
				}},
				{"key mint", func() *http.Response {
					return f.do(http.MethodPost, "/api/v1/principals/"+p.ID+"/keys", map[string]any{"name": "k"}, withBearer(c.key))
				}},
			}
			for _, a := range actions {
				resp := a.call()
				label := fmt.Sprintf("%s: %s on a %s-level target", c.name, a.name, tg.name)
				want, code := tg.want, c.code
				if a.name != "create" && c.key != f.ops && c.code != CodeInsufficientScope {
					// Only the owner delivers a credential for another
					// principal; the refusal comes before the ceiling.
					want, code = http.StatusForbidden, CodeCredentialForOtherPrincipal
				}
				// The body is read once: as the failure message, or as
				// the error code of an expected refusal.
				if resp.StatusCode != want {
					t.Errorf("%s: status %d, want %d: %s", label, resp.StatusCode, want, errBody(t, resp))
					continue
				}
				if want == http.StatusForbidden {
					assert.Equal(t, code, errCode(t, resp), label)
				}
			}
		}
	}
}
