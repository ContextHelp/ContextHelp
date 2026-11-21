package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// An administrator never receives a working credential for another
// principal (ADR-083 §10: administering an instance is not access to
// its data). A key secret, a sign-in link or an identity bound to a
// login the administrator controls would let it act as that principal
// and read what that principal is shown, so every route that yields
// one for a principal other than the caller is the owner's alone.

// deliveryRoute is a route that yields a working credential for the
// principal in its path.
type deliveryRoute struct {
	name   string
	method string
	// path is the route for the target principal.
	path func(id string) string
	// body is the request body; n makes it unique per call.
	body func(n int) any
	// ownerWant is the owner's status per target kind: what the route
	// answered the owner before this rule, unchanged by it.
	ownerWant map[string]int
}

func deliveryRoutes() []deliveryRoute {
	both := map[string]int{storage.PrincipalKindDevice: http.StatusCreated, storage.PrincipalKindPerson: http.StatusCreated}
	return []deliveryRoute{
		{
			name: "key mint", method: http.MethodPost,
			path:      func(id string) string { return "/api/v1/principals/" + id + "/keys" },
			body:      func(int) any { return map[string]any{"name": "takeover"} },
			ownerWant: both,
		},
		{
			name: "sign-in link", method: http.MethodPost,
			path:      func(id string) string { return "/api/v1/principals/" + id + "/links" },
			body:      func(int) any { return nil },
			ownerWant: both,
		},
		{
			name: "identity bind (proxy)", method: http.MethodPost,
			path: func(id string) string { return "/api/v1/principals/" + id + "/identities" },
			body: func(n int) any {
				return map[string]any{"method": "proxy", "issuer": "tailscale", "subject": fmt.Sprintf("mallory-%d@example.com", n)}
			},
			ownerWant: both,
		},
		{
			name: "identity bind (oidc)", method: http.MethodPost,
			path: func(id string) string { return "/api/v1/principals/" + id + "/identities" },
			body: func(n int) any {
				return map[string]any{"method": "oidc", "issuer": "https://idp.example", "subject": fmt.Sprintf("mallory-%d", n)}
			},
			ownerWant: both,
		},
	}
}

// credentialState is what a refused delivery must leave as it found
// it: the target's keys and identities.
func (f *uiFixture) credentialState(id string) (keys, identities int) {
	f.t.Helper()
	k, err := f.keys.ListByPrincipal(ownerCtx(), id)
	require.NoError(f.t, err)
	i, err := f.bundle.svc.Store(ownerCtx()).Auth().Identities().ListByPrincipal(ownerCtx(), id)
	require.NoError(f.t, err)
	return len(k), len(i)
}

// assertNoCredential checks a refusal is the error envelope alone: no
// key, no link, no identity, no ticket fragment.
func assertNoCredential(t *testing.T, raw []byte, label string) {
	t.Helper()
	var top map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &top), label)
	assert.Equal(t, []string{"error"}, keysOf(top), "%s: the refusal carries only the error", label)
	assert.NotContains(t, string(raw), "#t=", label)
	assert.NotContains(t, string(raw), publicURL, label)
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The matrix: every delivery route, for a device and a person target,
// as the owner (allowed, as before), as each non-owner administrator
// (refused 403 CREDENTIAL_FOR_OTHER_PRINCIPAL; INSUFFICIENT_SCOPE for a
// session that lacks admin:principals), with nothing delivered and the target's
// credentials untouched.
func TestCredentialDelivery_OnlyTheOwnerIssuesForAnother(t *testing.T) {
	f := newCeremonyFixture(t)
	callers := f.adminCallers()
	// A narrowed administrator: it holds admin:principals and less than
	// a writer device's bundle. The delivery rule answers before the
	// ceiling would.
	narrowID, narrow := f.adminKey("Narrow", authn.ScopeAdminPrincipals, authn.ScopeReadObjects, authn.ScopeReadSystem)
	callers = append(callers, adminCaller{
		name: "narrow key", opts: []reqOpt{withBearer(narrow)}, principal: narrowID,
		scopes: []authn.Scope{authn.ScopeAdminPrincipals, authn.ScopeReadObjects, authn.ScopeReadSystem},
	})

	n := 0
	target := func(kind string) storage.Principal {
		n++
		resp := f.createPrincipal(f.ops, kind, fmt.Sprintf("target %d", n))
		require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
		return decodeBody[storage.Principal](t, resp)
	}
	for _, route := range deliveryRoutes() {
		for _, kind := range []string{storage.PrincipalKindDevice, storage.PrincipalKindPerson} {
			p := target(kind)
			keysBefore, idnsBefore := f.credentialState(p.ID)
			for _, c := range callers {
				n++
				label := fmt.Sprintf("%s admin: %s for a %s", c.name, route.name, kind)
				resp := f.callAs(c, route.method, route.path(p.ID), route.body(n))
				raw, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				want, status := authn.CodeCredentialForOtherPrincipal, http.StatusForbidden
				switch {
				case !c.holds(authn.ScopeAdminPrincipals):
					want = CodeInsufficientScope
				}
				assert.Equal(t, status, resp.StatusCode, "%s: %s", label, raw)
				var env ErrorEnvelope
				require.NoError(t, json.Unmarshal(raw, &env), label)
				assert.Equal(t, want, env.Error.Code, label)
				assertNoCredential(t, raw, label)
			}
			keysAfter, idnsAfter := f.credentialState(p.ID)
			assert.Equal(t, keysBefore, keysAfter, "%s for a %s: an administrator added a key", route.name, kind)
			assert.Equal(t, idnsBefore, idnsAfter, "%s for a %s: an administrator bound an identity", route.name, kind)

			// The owner: as before.
			n++
			resp := f.do(route.method, route.path(p.ID), route.body(n), withBearer(f.ops))
			assert.Equal(t, route.ownerWant[kind], resp.StatusCode, "owner: %s for a %s: %s", route.name, kind, errBody(t, resp))
		}
	}
}

// The other principal is any principal but the caller: another
// administrator and a reader alike. The owner keeps its own guard and
// its code (OWNER_PROTECTED), tested in the admin boundary tests.
func TestCredentialDelivery_RefusedForAnotherAdministrator(t *testing.T) {
	f := newCeremonyFixture(t)
	_, dana := f.adminKey("Dana")
	eveID, _ := f.adminKey("Eve")
	for i, route := range deliveryRoutes() {
		for j, id := range []string{eveID, "other"} {
			resp := f.do(route.method, route.path(id), route.body(1000+10*i+j), withBearer(dana))
			raw, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			label := fmt.Sprintf("Dana: %s for %s", route.name, id)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s: %s", label, raw)
			assert.Contains(t, string(raw), authn.CodeCredentialForOtherPrincipal, label)
			assertNoCredential(t, raw, label)
		}
	}
	// No such principal: the link refusal comes before any lookup.
	resp := f.do(http.MethodPost, "/api/v1/principals/prn_nobody/links", nil, withBearer(dana))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, authn.CodeCredentialForOtherPrincipal, errCode(t, resp))
}

// A credential for oneself is no delivery to anyone else: an
// administrator mints its own key and sign-in link through the admin
// routes and the /self routes alike, and a target principal still
// manages its own credentials under write:self.
func TestCredentialDelivery_SelfIsUnchanged(t *testing.T) {
	f := newCeremonyFixture(t)
	adminID, admin := f.adminKey("Self admin")
	resp := f.do(http.MethodPost, "/api/v1/principals/"+adminID+"/keys", map[string]any{"name": "mine"}, withBearer(admin))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.NotEmpty(t, decodeBody[createdKey](t, resp).Key)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+adminID+"/links", nil, withBearer(admin))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.Contains(t, decodeBody[linkResponse](t, resp).URL, "#t=")
	resp = f.do(http.MethodPost, "/api/v1/principals/"+adminID+"/identities",
		map[string]any{"method": "proxy", "issuer": "tailscale", "subject": "self-admin@example.com"}, withBearer(admin))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))

	// The target device, with the key the owner minted it: its own key,
	// its own login code, its own lists.
	resp = f.createPrincipal(f.ops, storage.PrincipalKindDevice, "Target device")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	device := decodeBody[storage.Principal](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/keys", map[string]any{"name": "first"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	deviceKey := decodeBody[createdKey](t, resp).Key
	resp = f.do(http.MethodPost, "/api/v1/self/keys", map[string]any{"name": "second"}, withBearer(deviceKey))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, device.ID, decodeBody[createdKey](t, resp).PrincipalID)
	assert.NotEmpty(t, f.mint(deviceKey), "the device signs a browser in as itself")
	for _, path := range []string{"/api/v1/self/keys", "/api/v1/self/identities", "/api/v1/self/sessions"} {
		assert.Equal(t, http.StatusOK, f.do(http.MethodGet, path, nil, withBearer(deviceKey)).StatusCode, path)
	}

	// The target person, signed in through the owner's link: its own
	// identities and sessions.
	resp = f.createPrincipal(f.ops, storage.PrincipalKindPerson, "Target person")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	person := decodeBody[storage.Principal](t, resp)
	resp = f.signInLink(f.ops, person.ID)
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	resp = f.redeemLink(secretOf(t, decodeBody[linkResponse](t, resp).URL))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	cookie := sessionCookieOf(t, resp)
	for _, path := range []string{"/api/v1/self/identities", "/api/v1/self/sessions"} {
		assert.Equal(t, http.StatusOK, f.do(http.MethodGet, path, nil, withCookie(cookie), fromUI).StatusCode, path)
	}
}

// Administering stays an administrator's: it creates principals, reads
// their credential metadata, removes their keys and identities,
// suspends and revokes them. None of it hands over a credential, and
// no answer carries a secret.
func TestCredentialDelivery_AdministrationIsUnchanged(t *testing.T) {
	f := newCeremonyFixture(t)
	_, admin := f.adminKey("Admin")
	resp := f.createPrincipal(admin, storage.PrincipalKindDevice, "Managed")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	device := decodeBody[storage.Principal](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/keys", map[string]any{"name": "by the owner"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	minted := decodeBody[createdKey](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/identities",
		map[string]any{"method": "proxy", "issuer": "tailscale", "subject": "managed@example.com"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	proxy := decodeBody[storage.Identity](t, resp)

	for _, path := range []string{"", "/keys", "/identities"} {
		resp := f.do(http.MethodGet, "/api/v1/principals/"+device.ID+path, nil, withBearer(admin))
		require.Equal(t, http.StatusOK, resp.StatusCode, path)
		body := rawBody(t, resp)
		assert.NotContains(t, body, minted.Key, "GET %s carries the key", path)
		assert.NotContains(t, strings.ToLower(body), "hash", path)
	}
	resp = f.do(http.MethodDelete, "/api/v1/principals/"+device.ID+"/identities/"+proxy.ID, nil, withBearer(admin))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodDelete, "/api/v1/principals/"+device.ID+"/keys/"+minted.ID, nil, withBearer(admin))
	assert.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/suspend", nil, withBearer(admin))
	assert.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodPost, "/api/v1/principals/"+device.ID+"/revoke", nil, withBearer(admin))
	assert.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
}

// An invite is a credential for a principal that does not exist yet:
// redeeming it creates a person, in the invite's audience when it
// names one, and signs the redeemer in as that person. So it is the
// owner's to mint, like a key or a link for another principal: each
// administrator, the browser session holding admin:shares included, is
// refused 403 CREDENTIAL_FOR_OTHER_PRINCIPAL with or without an
// audience, and the refusal carries no link. The owner's invite is
// minted and redeemed as before.
func TestCredentialDelivery_InvitesAreTheOwners(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.createAudience(f.ops, "partners")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	callers := f.adminCallers()
	// An administrator without admin:shares never reaches the route.
	narrowID, narrow := f.adminKey("Narrow", authn.ScopeAdminPrincipals, authn.ScopeReadSystem)
	callers = append(callers, adminCaller{
		name: "narrow key", opts: []reqOpt{withBearer(narrow)}, principal: narrowID,
		scopes: []authn.Scope{authn.ScopeAdminPrincipals, authn.ScopeReadSystem},
	})
	people := func() int {
		list, err := f.bundle.svc.Store(ownerCtx()).Auth().Principals().List(ownerCtx(), storage.PrincipalFilter{Kind: storage.PrincipalKindPerson})
		require.NoError(t, err)
		return len(list)
	}
	before := people()
	for _, c := range callers {
		for _, body := range []map[string]any{
			{"display_name": "Mallory " + c.name},
			{"display_name": "Mallory " + c.name, "audience": "partners"},
		} {
			label := fmt.Sprintf("%s admin: invite %v", c.name, body)
			resp := f.callAs(c, http.MethodPost, "/api/v1/share/invites", body)
			raw, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			want := authn.CodeCredentialForOtherPrincipal
			if !c.holds(authn.ScopeAdminShares) {
				want = CodeInsufficientScope
			}
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, "%s: %s", label, raw)
			var env ErrorEnvelope
			require.NoError(t, json.Unmarshal(raw, &env), label)
			assert.Equal(t, want, env.Error.Code, label)
			assertNoCredential(t, raw, label)
		}
	}
	assert.Equal(t, before, people(), "no administrator's invite created anyone")

	// The owner, as before: minted, redeemed, in the audience.
	resp = f.do(http.MethodPost, "/api/v1/share/invites", map[string]any{"display_name": "Bob", "audience": "partners"}, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	resp = f.redeemInvite(secretOf(t, decodeBody[linkResponse](t, resp).URL))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, before+1, people())
}

// The refusal does not depend on the body: a non-owner administrator
// sending a malformed or invalid body to a key or identity route still
// gets 403 CREDENTIAL_FOR_OTHER_PRINCIPAL, never a validation answer
// that would tell it what the owner's request would be checked against.
func TestCredentialDelivery_RefusalComesBeforeTheBody(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.createPrincipal(f.ops, storage.PrincipalKindDevice, "target")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	target := decodeBody[storage.Principal](t, resp)
	for _, c := range f.adminCallers() {
		if !c.holds(authn.ScopeAdminPrincipals) {
			continue
		}
		for _, call := range []struct {
			name, path string
			body       any
		}{
			{"key, unknown scope", "/api/v1/principals/" + target.ID + "/keys", map[string]any{"scopes": []string{"no:such"}}},
			{"key, bad expiry", "/api/v1/principals/" + target.ID + "/keys", map[string]any{"expires": "never"}},
			{"key, not JSON", "/api/v1/principals/" + target.ID + "/keys", "not an object"},
			{"identity, no method", "/api/v1/principals/" + target.ID + "/identities", map[string]any{}},
			{"identity, bad method", "/api/v1/principals/" + target.ID + "/identities", map[string]any{"method": "key", "subject": "x"}},
			{"identity, not JSON", "/api/v1/principals/" + target.ID + "/identities", "not an object"},
		} {
			label := c.name + ": " + call.name
			resp := f.callAs(c, http.MethodPost, call.path, call.body)
			require.Equal(t, http.StatusForbidden, resp.StatusCode, label)
			assert.Equal(t, authn.CodeCredentialForOtherPrincipal, errCode(t, resp), label)
		}
	}
}
