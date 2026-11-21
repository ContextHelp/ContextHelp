package http

import (
	"net/http"
	"slices"

	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The two kinds of administrator an instance has: a device presenting a
// key with the whole admin bundle, and a person signed in to the web
// UI, whose session carries the UI scope set intersected with the
// bundle.

// adminCaller is one way of calling the API as an administrator.
type adminCaller struct {
	name string
	// opts authenticate a request.
	opts []reqOpt
	// scopes are what the credential holds.
	scopes []authn.Scope
	// principal is the administrator's registry id.
	principal string
}

func (c adminCaller) holds(s authn.Scope) bool { return slices.Contains(c.scopes, s) }

// adminPerson creates a person holding the admin role, signs it in
// through a sign-in link the owner mints and returns its session
// cookie.
func (f *uiFixture) adminPerson(name string) (string, *http.Cookie) {
	f.t.Helper()
	resp := f.createPrincipal(f.ops, storage.PrincipalKindPerson, name, authn.RoleAdmin)
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	p := decodeBody[storage.Principal](f.t, resp)
	resp = f.signInLink(f.ops, p.ID)
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	resp = f.redeemLink(secretOf(f.t, decodeBody[linkResponse](f.t, resp).URL))
	require.Equal(f.t, http.StatusOK, resp.StatusCode, errBody(f.t, resp))
	return p.ID, sessionCookieOf(f.t, resp)
}

// adminCallers returns both kinds of administrator.
func (f *uiFixture) adminCallers() []adminCaller {
	f.t.Helper()
	keyID, key := f.adminKey("Key admin")
	personID, cookie := f.adminPerson("Session admin")
	return []adminCaller{
		{name: "key", opts: []reqOpt{withBearer(key)}, scopes: authn.ScopesForRoles([]string{authn.RoleAdmin}), principal: keyID},
		{name: "session", opts: []reqOpt{withCookie(cookie), fromUI},
			scopes: []authn.Scope{authn.ScopeAdminShares, authn.ScopeWriteSelf}, principal: personID},
	}
}

// call sends one request as the administrator.
func (f *uiFixture) callAs(c adminCaller, method, path string, body any) *http.Response {
	f.t.Helper()
	return f.do(method, path, body, c.opts...)
}
