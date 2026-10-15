package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// testClock is a settable, concurrency-safe clock.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// uiPrincipal is a principal the fixture creates in the registry, with
// one API key.
type uiPrincipal struct {
	ID    string
	Kind  string
	Roles []string
}

// uiFixture is an authenticated dpkms router with web UI sign-in on,
// over key-backed principals: the owner "ops" (admin) and the person
// "other" (reader) by default.
type uiFixture struct {
	t        *testing.T
	bundle   *testServerBundle
	clock    *testClock
	keys     *authn.Keys
	sessions *authn.Sessions
	ts       *httptest.Server
	origin   string
	// tokens maps a principal id to its minted API key.
	tokens map[string]string
	// ops and other are the default principals' keys.
	ops, other string
}

func newUIFixture(t *testing.T, principals ...uiPrincipal) *uiFixture {
	t.Helper()
	bundle := newTestServerBundle(t)
	t.Cleanup(bundle.Close)
	f := &uiFixture{t: t, bundle: bundle, clock: &testClock{t: time.Now().UTC().Truncate(time.Millisecond)}, tokens: map[string]string{}}
	if len(principals) == 0 {
		principals = []uiPrincipal{
			{ID: "ops", Kind: storage.PrincipalKindOwner, Roles: []string{authn.RoleAdmin}},
			{ID: "other", Kind: storage.PrincipalKindPerson, Roles: []string{authn.RoleReader}},
		}
	}
	auth := bundle.svc.Store(ownerCtx()).Auth()
	f.keys = authn.NewKeys(authn.NewLocalKeys(auth.KV(), f.clock.now), auth, f.clock.now)
	for _, p := range principals {
		id := p.ID
		if p.Kind == storage.PrincipalKindOwner {
			id = storage.OwnerPrincipal
		} else {
			require.NoError(t, auth.Principals().Create(ownerCtx(), &storage.Principal{ID: p.ID, Kind: p.Kind, DisplayName: p.ID, Roles: p.Roles}))
		}
		secret, _, err := f.keys.Mint(ownerCtx(), authn.MintKey{PrincipalID: id, Name: p.ID})
		require.NoError(t, err)
		f.tokens[p.ID] = secret
	}
	f.ops, f.other = f.tokens["ops"], f.tokens["other"]
	f.start()
	return f
}

// start (re)builds sessions, the chain and the router over the same
// store: what a dpkms restart does.
func (f *uiFixture) start() {
	f.t.Helper()
	var err error
	f.sessions, err = authn.NewSessions(f.bundle.svc.Store(ownerCtx()).UISessions(), f.bundle.svc.Store(ownerCtx()).Auth(), f.keys, authn.SessionOptions{Now: f.clock.now})
	require.NoError(f.t, err)
	if f.ts != nil {
		f.ts.Close()
	}
	f.ts = httptest.NewServer(NewRouterWithConfig(f.bundle.svc, RouterConfig{
		Auth:           authn.NewChain(authn.NewAPIKeyMethod(f.keys, nil), f.sessions.Method()),
		Sessions:       f.sessions,
		SessionRecheck: 20 * time.Millisecond,
	}))
	f.t.Cleanup(f.ts.Close)
	f.origin = f.ts.URL
}

// revokeKey revokes the API key of principal id.
func (f *uiFixture) revokeKey(id string) {
	f.t.Helper()
	p, ok, err := authn.NewAPIKeyMethod(f.keys, nil).Authenticate(ownerCtx(), authn.Credential{Scheme: authn.SchemeBearer, Token: f.tokens[id]})
	require.NoError(f.t, err)
	require.True(f.t, ok)
	require.NoError(f.t, f.keys.Revoke(ownerCtx(), p.Meta[authn.MetaKeyID]))
}

func (f *uiFixture) host() string { return strings.TrimPrefix(f.ts.URL, "http://") }

type reqOpt func(*http.Request)

func withBearer(tok string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }

// fromUI marks a request the way the web UI's fetch sends it.
func fromUI(r *http.Request) {
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		r.Header.Set("Origin", strings.TrimSuffix(r.URL.Scheme+"://"+r.URL.Host, "/"))
		r.Header.Set(HeaderCSRF, "1")
	}
}

func withHeader(k, v string) reqOpt { return func(r *http.Request) { r.Header.Set(k, v) } }

func withoutHeader(k string) reqOpt { return func(r *http.Request) { r.Header.Del(k) } }

func (f *uiFixture) do(method, path string, body any, opts ...reqOpt) *http.Response {
	f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(f.t, err)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rd)
	require.NoError(f.t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(f.t, err)
	f.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func decodeBody[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&v))
	return v
}

func errCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	return decodeBody[ErrorEnvelope](t, resp).Error.Code
}

func (f *uiFixture) mint(tok string) string {
	f.t.Helper()
	resp := f.do(http.MethodPost, "/api/v1/ui/login-codes", nil, withBearer(tok))
	require.Equal(f.t, http.StatusCreated, resp.StatusCode)
	return decodeBody[loginCodeResponse](f.t, resp).Code
}

func (f *uiFixture) exchange(code string, opts ...reqOpt) *http.Response {
	f.t.Helper()
	return f.do(http.MethodPost, "/ui/auth/session", map[string]string{"code": code}, append([]reqOpt{fromUI}, opts...)...)
}

func sessionCookieOf(t *testing.T, resp *http.Response) *http.Cookie {
	t.Helper()
	for _, c := range resp.Cookies() {
		if strings.HasPrefix(c.Name, sessionCookiePrefix) {
			return c
		}
	}
	t.Fatalf("no session cookie in %v", resp.Header.Values("Set-Cookie"))
	return nil
}

func (f *uiFixture) signIn() *http.Cookie {
	f.t.Helper()
	resp := f.exchange(f.mint(f.ops))
	require.Equal(f.t, http.StatusOK, resp.StatusCode)
	return sessionCookieOf(f.t, resp)
}

func TestUISignIn_MintAndExchange(t *testing.T) {
	f := newUIFixture(t)
	resp := f.do(http.MethodPost, "/api/v1/ui/login-codes", nil, withBearer(f.ops))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	lc := decodeBody[loginCodeResponse](t, resp)
	assert.True(t, lc.SessionRequired)
	assert.Len(t, lc.Code, 43)
	assert.Equal(t, LoginPath, lc.LoginPath)
	assert.Equal(t, 60, lc.ExpiresIn)

	resp = f.exchange(lc.Code)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	who := decodeBody[exchangeResponse](t, resp)
	assert.Equal(t, storage.OwnerPrincipal, who.Principal)
	assert.Equal(t, authn.ViaSession, who.Via)
	assert.Equal(t, authn.UISessionScopes, who.Scopes)
	require.NotNil(t, who.Session)
	assert.Equal(t, authn.SessionKindUI, who.Session.Kind)
	assert.Empty(t, who.Warning, "loopback sign-in raises no plain-HTTP warning")
}

func TestUISignIn_CookieAttributes(t *testing.T) {
	f := newUIFixture(t)
	resp := f.exchange(f.mint(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw := resp.Header.Get("Set-Cookie")
	c := sessionCookieOf(t, resp)

	assert.Equal(t, SessionCookieName(f.host()), c.Name)
	assert.True(t, strings.HasPrefix(c.Name, "__Host-"), "__Host- prefix pins Secure, Path=/ and no Domain")
	assert.True(t, c.HttpOnly, "HttpOnly")
	assert.True(t, c.Secure, "Secure")
	assert.Equal(t, http.SameSiteStrictMode, c.SameSite)
	assert.Equal(t, "/", c.Path)
	assert.Empty(t, c.Domain)
	assert.NotContains(t, strings.ToLower(raw), "domain=")
	assert.Len(t, c.Value, 43)
	assert.WithinDuration(t, time.Now().Add(authn.DefaultSessionMaxTTL), c.Expires, time.Minute)
}

func TestSessionCookieName_PerInstance(t *testing.T) {
	a := SessionCookieName("127.0.0.1:8080")
	b := SessionCookieName("127.0.0.1:8081")
	assert.NotEqual(t, a, b, "two ports on one host share a cookie jar")
	assert.Equal(t, a, SessionCookieName("127.0.0.1:8080"))
	assert.Equal(t, SessionCookieName("dpkms.lan:8443"), SessionCookieName("DPKMS.lan:8443"))
	for _, n := range []string{a, b} {
		assert.Regexp(t, `^__Host-dpkms_[0-9a-f]{16}$`, n)
	}
}

func TestUISession_CookieAuthenticatesAndBearerUnchanged(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()

	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	who := decodeBody[whoamiResponse](t, resp)
	assert.Equal(t, storage.OwnerPrincipal, who.Principal)
	assert.Equal(t, authn.ViaSession, who.Via)
	require.NotNil(t, who.Session)

	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.ops))
	who = decodeBody[whoamiResponse](t, resp)
	assert.Equal(t, "token", who.Via)
	assert.Nil(t, who.Session)

	// Bearer writes need no CSRF header and no browser headers.
	resp = f.do(http.MethodDelete, "/api/v1/objects/missing", nil, withBearer(f.ops))
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/audit-log", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUISession_RejectedCookies(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()

	unknown := &http.Cookie{Name: c.Name, Value: strings.Repeat("A", 43)}
	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(unknown), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "unknown cookie")

	other := &http.Cookie{Name: SessionCookieName("127.0.0.1:1"), Value: c.Value}
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(other), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "another instance's cookie name")

	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withBearer(c.Value))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "cookie secret sent as bearer")

	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), withBearer("tok-wrong"), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a header credential wins over the cookie")
}

func TestUISession_IdleExpiredCookie401(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	f.clock.advance(authn.DefaultSessionIdleTTL)
	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUISession_MaxExpiredCookie401(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	for elapsed := time.Duration(0); elapsed < authn.DefaultSessionMaxTTL-12*time.Hour; elapsed += 11 * time.Hour {
		f.clock.advance(11 * time.Hour)
		resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
		require.Equal(t, http.StatusOK, resp.StatusCode, "active session refused after %s", elapsed)
	}
	f.clock.advance(12 * time.Hour)
	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestUISession_SignOutRevokes(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	resp := f.do(http.MethodDelete, "/api/v1/ui/session", nil, withCookie(c), fromUI)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	var cleared *http.Cookie
	for _, sc := range resp.Cookies() {
		if sc.Name == c.Name {
			cleared = sc
		}
	}
	require.NotNil(t, cleared, "sign-out clears the cookie")
	assert.Negative(t, cleared.MaxAge)
	assert.Empty(t, cleared.Value)

	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "revoked cookie")

	// signout:ui is session-only: even an admin token lacks it.
	resp = f.do(http.MethodDelete, "/api/v1/ui/session", nil, withBearer(f.ops))
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	env := decodeBody[ErrorEnvelope](t, resp)
	assert.Equal(t, CodeInsufficientScope, env.Error.Code)
	assert.Equal(t, string(authn.ScopeSignoutUI), env.Error.Details["required_scope"])
}

// A restart keeps a session; revoking the key that minted it ends it,
// and the stale cookie is cleared on its next request.
func TestUISession_RevokedKeyEndsSession(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()

	// A restart over the same store keeps the session: the control
	// that makes the refusal below mean something.
	f.start()
	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(f.sameCookie(c)), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode, "session survives a restart")

	f.revokeKey("ops")
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(f.sameCookie(c)), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	cleared := clearedCookie(t, resp, SessionCookieName(f.host()))
	require.NotNil(t, cleared, "a dead cookie is cleared")
	assert.Negative(t, cleared.MaxAge)
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withBearer(f.ops))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "the revoked key itself is refused")
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withBearer(f.other))
	assert.Equal(t, http.StatusOK, resp.StatusCode, "another principal's key is untouched")
}

// clearedCookie returns the Set-Cookie named name in resp, or nil.
func clearedCookie(t *testing.T, resp *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, sc := range resp.Cookies() {
		if sc.Name == name {
			return sc
		}
	}
	return nil
}

// Key beside cookie: the carried credential decides, in both
// directions. A valid key of another principal with a valid cookie is
// that key's principal, with no browser rules; a bad key with a valid
// cookie is refused, and the cookie is left alone.
func TestUISession_KeyBesideCookie(t *testing.T) {
	f := newUIFixture(t)
	c := f.signIn()
	who := decodeBody[whoamiResponse](t, f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), withBearer(f.other)))
	assert.Equal(t, "other", who.Principal)
	assert.Equal(t, viaToken, who.Via)
	resp := f.do(http.MethodDelete, "/api/v1/objects/missing", nil, withCookie(c), withBearer(f.ops))
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode, "a key lifts the browser rules")

	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), withBearer("ctxt_bad"), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, clearedCookie(t, resp, SessionCookieName(f.host())), "a refused key does not touch the cookie")
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the session is intact")
}

// An unknown or expired cookie is 401 and cleared; a malformed one is
// 401 and left alone, since it fails its integrity check rather than
// having expired.
func TestUISession_StaleCookieCleared(t *testing.T) {
	f := newUIFixture(t)
	name := SessionCookieName(f.host())
	resp := f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(&http.Cookie{Name: name, Value: strings.Repeat("A", 43)}), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NotNil(t, clearedCookie(t, resp, name), "an unknown cookie is cleared")

	c := f.signIn()
	f.clock.advance(authn.DefaultSessionIdleTTL)
	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	require.NotNil(t, clearedCookie(t, resp, name), "an expired cookie is cleared")

	resp = f.do(http.MethodGet, "/api/v1/objects", nil, withCookie(&http.Cookie{Name: name, Value: "nope"}), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Nil(t, clearedCookie(t, resp, name), "a malformed cookie is refused, not cleared")
}

// sameCookie is c as the browser would send it to the restarted test
// server, whose new port gives the cookie a new name.
func (f *uiFixture) sameCookie(c *http.Cookie) *http.Cookie {
	return &http.Cookie{Name: SessionCookieName(f.host()), Value: c.Value}
}

func TestUICodeExchange(t *testing.T) {
	f := newUIFixture(t)

	t.Run("single use", func(t *testing.T) {
		code := f.mint(f.ops)
		require.Equal(t, http.StatusOK, f.exchange(code).StatusCode)
		resp := f.exchange(code)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		assert.Equal(t, "INVALID_LOGIN_CODE", errCode(t, resp))
		assert.Empty(t, resp.Header.Values("Set-Cookie"))
	})
	t.Run("expired", func(t *testing.T) {
		code := f.mint(f.ops)
		f.clock.advance(authn.DefaultLoginCodeTTL)
		resp := f.exchange(code)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
	t.Run("wrong code", func(t *testing.T) {
		resp := f.exchange(strings.Repeat("x", 43))
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		resp = f.exchange("")
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
	t.Run("needs the CSRF header", func(t *testing.T) {
		code := f.mint(f.ops)
		resp := f.exchange(code, withoutHeader(HeaderCSRF))
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, "CSRF_HEADER_REQUIRED", errCode(t, resp))
		// The refused attempt did not spend the code.
		assert.Equal(t, http.StatusOK, f.exchange(code).StatusCode)
	})
	t.Run("cross-site refused", func(t *testing.T) {
		code := f.mint(f.ops)
		for name, opts := range map[string][]reqOpt{
			"cross-site":     {withHeader("Sec-Fetch-Site", "cross-site"), withHeader("Origin", "https://evil.example")},
			"same-site port": {withHeader("Sec-Fetch-Site", "same-site"), withHeader("Origin", "http://127.0.0.1:1")},
			"no browser hdr": {withoutHeader("Sec-Fetch-Site"), withoutHeader("Origin")},
		} {
			resp := f.exchange(code, opts...)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode, name)
		}
		assert.Equal(t, http.StatusOK, f.exchange(code).StatusCode)
	})
	t.Run("bearer cannot be exchanged", func(t *testing.T) {
		resp := f.exchange(f.ops)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
	t.Run("mint needs a token", func(t *testing.T) {
		resp := f.do(http.MethodPost, "/api/v1/ui/login-codes", nil)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		c := f.signIn()
		resp = f.do(http.MethodPost, "/api/v1/ui/login-codes", nil, withCookie(c), fromUI)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a session cannot mint sessions")
	})
}

func TestUISignIn_PrivateInstance(t *testing.T) {
	bundle := newTestServerBundle(t)
	defer bundle.Close()
	ts := httptest.NewServer(NewRouterWithConfig(bundle.svc, RouterConfig{}))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/ui/login-codes", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	lc := decodeBody[loginCodeResponse](t, resp)
	assert.False(t, lc.SessionRequired)
	assert.Empty(t, lc.Code)

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/ui/auth/session", strings.NewReader(`{"code":"x"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	fromUI(req)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
}

func TestUISignIn_PlainHTTPRemoteWarns(t *testing.T) {
	f := newUIFixture(t)
	code := f.mint(f.ops)
	var warned bytes.Buffer
	routes := newUISessionRoutes(RouterConfig{Sessions: f.sessions})
	routes.warn = &warned

	req := httptest.NewRequest(http.MethodPost, "http://dpkms.example.net/ui/auth/session", strings.NewReader(`{"code":"`+code+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(HeaderCSRF, "1")
	rr := httptest.NewRecorder()
	routes.exchange(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var who exchangeResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &who))
	assert.Equal(t, plainHTTPWarning, who.Warning)
	assert.Contains(t, warned.String(), "plain HTTP")

	for host, want := range map[string]bool{
		"localhost:8080": false, "127.0.0.1:8080": false, "[::1]:8080": false, "app.localhost": false,
		"dpkms.lan": true, "192.168.1.5:8080": true,
	} {
		r := httptest.NewRequest(http.MethodPost, "http://"+host+"/", nil)
		assert.Equal(t, want, plainHTTPRemote(r), host)
	}
	r := httptest.NewRequest(http.MethodPost, "http://dpkms.lan/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	assert.False(t, plainHTTPRemote(r), "behind a TLS proxy")
}
