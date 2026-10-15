package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// publicURL is the server.public_url the ceremony fixture builds its
// links on: links point at the public name, never the test listener.
const publicURL = "https://dpkms.example.net"

// ceremonyAttempts is the per-address budget the fixture's limiter
// grants: enough for the longest flow in one test, small enough that
// the rate-limit test reaches the N+1th attempt quickly.
const ceremonyAttempts = 20

// startCeremonies rebuilds the fixture's router with the ticket
// ceremonies on: sign_in as given, the principal registry wired and a
// small per-address budget.
func (f *uiFixture) startCeremonies(signIn ...string) {
	f.t.Helper()
	store := f.bundle.svc.Store(ownerCtx())
	var err error
	f.sessions, err = authn.NewSessions(store.UISessions(), store.Auth(), f.keys, authn.SessionOptions{Now: f.clock.now})
	require.NoError(f.t, err)
	tickets, err := authn.NewTickets(store.Auth(), f.sessions, f.clock.now)
	require.NoError(f.t, err)
	if f.ts != nil {
		f.ts.Close()
	}
	f.ts = httptest.NewServer(NewRouterWithConfig(f.bundle.svc, RouterConfig{
		Auth:            authn.NewChain(authn.NewAPIKeyMethod(f.keys, nil), f.sessions.Method()),
		Sessions:        f.sessions,
		SessionRecheck:  20 * time.Millisecond,
		Registry:        store.Auth(),
		Keys:            f.keys,
		Sharing:         store.Sharing(),
		Tickets:         tickets,
		SignIn:          signIn,
		PublicURL:       publicURL,
		CeremonyLimiter: NewAddrLimiter(ceremonyAttempts, time.Minute, f.clock.now),
		// Non-private instances take pushes from peer principals only.
		RequirePeerPrincipal: f.requirePeer,
	}))
	f.t.Cleanup(f.ts.Close)
	f.origin = f.ts.URL
}

func newCeremonyFixture(t *testing.T) *uiFixture {
	t.Helper()
	f := newUIFixture(t)
	f.startCeremonies(config.AuthSignInInvite, config.AuthSignInLink)
	return f
}

// secretOf lifts the ticket out of a link's fragment.
func secretOf(t *testing.T, link string) string {
	t.Helper()
	_, frag, ok := strings.Cut(link, "#t=")
	require.True(t, ok, "link %q carries no #t= fragment", link)
	return frag
}

func (f *uiFixture) mintInvite(tok, name string, expiresIn int) *http.Response {
	f.t.Helper()
	body := map[string]any{"display_name": name}
	if expiresIn != 0 {
		body["expires_in"] = expiresIn
	}
	return f.do(http.MethodPost, "/api/v1/share/invites", body, withBearer(tok))
}

func (f *uiFixture) inviteLink(name string) linkResponse {
	f.t.Helper()
	resp := f.mintInvite(f.ops, name, 0)
	require.Equal(f.t, http.StatusCreated, resp.StatusCode, errBody(f.t, resp))
	return decodeBody[linkResponse](f.t, resp)
}

func (f *uiFixture) redeemInvite(secret string, opts ...reqOpt) *http.Response {
	f.t.Helper()
	return f.do(http.MethodPost, InvitePath+"/session", map[string]string{"ticket": secret}, append([]reqOpt{fromUI}, opts...)...)
}

func (f *uiFixture) redeemLink(secret string, opts ...reqOpt) *http.Response {
	f.t.Helper()
	return f.do(http.MethodPost, SignInPath+"/session", map[string]string{"ticket": secret}, append([]reqOpt{fromUI}, opts...)...)
}

func (f *uiFixture) signInLink(tok, principal string) *http.Response {
	f.t.Helper()
	return f.do(http.MethodPost, "/api/v1/principals/"+principal+"/links", nil, withBearer(tok))
}

// acceptInvite mints an invite for name and redeems it: the person's
// session cookie and whoami.
func (f *uiFixture) acceptInvite(name string) (*http.Cookie, exchangeResponse) {
	f.t.Helper()
	resp := f.redeemInvite(secretOf(f.t, f.inviteLink(name).URL))
	require.Equal(f.t, http.StatusOK, resp.StatusCode, errBody(f.t, resp))
	return sessionCookieOf(f.t, resp), decodeBody[exchangeResponse](f.t, resp)
}

// errBody renders a failed response's error envelope for an assertion
// message; a success leaves the body untouched for the test to decode.
func errBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode < 300 {
		return ""
	}
	var env ErrorEnvelope
	_ = json.NewDecoder(resp.Body).Decode(&env)
	return env.Error.Code + ": " + env.Error.Message
}

func TestInvite_MintRedeemSessionWhoami(t *testing.T) {
	f := newCeremonyFixture(t)

	resp := f.mintInvite(f.ops, "Bob Partner", 0)
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	link := decodeBody[linkResponse](t, resp)
	assert.True(t, strings.HasPrefix(link.URL, publicURL+InvitePath+"#t="), "link %q", link.URL)
	assert.Len(t, secretOf(t, link.URL), 43)
	assert.Equal(t, int(authn.DefaultInviteTTL.Seconds()), link.ExpiresIn)
	assert.WithinDuration(t, f.clock.now().Add(authn.DefaultInviteTTL), link.ExpiresAt, time.Second)

	resp = f.redeemInvite(secretOf(t, link.URL))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	c := sessionCookieOf(t, resp)
	signed := decodeBody[exchangeResponse](t, resp)
	assert.True(t, strings.HasPrefix(signed.Principal, "prn_"), "a fresh person, got %q", signed.Principal)
	assert.Equal(t, "Bob Partner", signed.Name)
	assert.Equal(t, []string{authn.RoleReader}, signed.Roles)
	assert.Equal(t, authn.ViaSession, signed.Via)
	require.NotNil(t, signed.Session)

	// The cookie is the person's: whoami names them, reads are the
	// reader set narrowed to the web UI's, and nothing else is reachable.
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	who := decodeBody[whoamiResponse](t, resp)
	assert.Equal(t, signed.Principal, who.Principal)
	assert.Equal(t, storage.PrincipalKindPerson, principalKind(t, f, who.Principal))
	assert.Equal(t, []string{authn.RoleReader}, who.Roles)
	assert.Equal(t, authn.SessionScopesFor(authn.ScopesForRoles([]string{authn.RoleReader}), authn.SessionKindUI), who.Scopes)
	assert.NotContains(t, who.Scopes, authn.ScopeAdminPrincipals)
	resp = f.do(http.MethodGet, "/api/v1/principals", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "a reader's session holds no admin scope")

	// The session is bound to a link identity whose subject is the
	// ticket's hash.
	store := f.bundle.svc.Store(ownerCtx()).Auth()
	sess, err := f.sessions.Get(ownerCtx(), who.Session.ID)
	require.NoError(t, err)
	idn, err := store.Identities().Get(ownerCtx(), sess.IdentityID)
	require.NoError(t, err)
	assert.Equal(t, storage.IdentityMethodLink, idn.Method)
	assert.Equal(t, storage.TicketHash(secretOf(t, link.URL)), idn.Subject)
	assert.Equal(t, authn.LinkIdentityInvite, idn.Name)
}

func principalKind(t *testing.T, f *uiFixture, id string) string {
	t.Helper()
	p, err := f.bundle.svc.Store(ownerCtx()).Auth().Principals().Get(ownerCtx(), id)
	require.NoError(t, err)
	return p.Kind
}

func TestInvite_SecondRedemptionRefused(t *testing.T) {
	f := newCeremonyFixture(t)
	secret := secretOf(t, f.inviteLink("Bob").URL)
	resp := f.redeemInvite(secret)
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))

	// A second browser (no cookie) with the same link.
	resp = f.redeemInvite(secret)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, CodeInvalidTicket, errCode(t, resp))
	assert.Empty(t, resp.Cookies(), "no cookie on a refusal")
}

func TestInvite_RedemptionWithActiveSessionRefused(t *testing.T) {
	f := newCeremonyFixture(t)
	owner := f.signIn()
	secret := secretOf(t, f.inviteLink("Bob").URL)

	resp := f.redeemInvite(secret, withCookie(owner))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodeSessionActive, errCode(t, resp))

	// The ticket was not spent: after signing out it redeems.
	resp = f.do(http.MethodDelete, "/api/v1/ui/session", nil, withCookie(owner), fromUI)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	resp = f.redeemInvite(secret, withCookie(owner))
	assert.Equal(t, http.StatusOK, resp.StatusCode, "a stale cookie is not a session: %s", errBody(t, resp))

	// The same rule holds for the login-code exchange.
	code := f.mint(f.ops)
	person, _ := f.acceptInvite("Carol")
	resp = f.exchange(code, withCookie(person))
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodeSessionActive, errCode(t, resp))
	resp = f.exchange(code)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the code was not spent by the refusal")
}

func TestInvite_ExpiredRefused(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.mintInvite(f.ops, "Bob", 3600)
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	link := decodeBody[linkResponse](t, resp)
	assert.Equal(t, 3600, link.ExpiresIn)
	f.clock.advance(time.Hour)
	resp = f.redeemInvite(secretOf(t, link.URL))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, CodeInvalidTicket, errCode(t, resp))
}

func TestInvite_MintRefusals(t *testing.T) {
	f := newCeremonyFixture(t)

	resp := f.mintInvite(f.other, "Bob", 0)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode, "admin:shares is the admin bundle's")
	assert.Equal(t, CodeInsufficientScope, errCode(t, resp))

	resp = f.mintInvite(f.ops, "   ", 0)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	resp = f.mintInvite(f.ops, "Bob", -1)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Minting needs the ceremony enabled.
	f.startCeremonies(config.AuthSignInLink)
	resp = f.mintInvite(f.ops, "Bob", 0)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, CodeSignInDisabled, errCode(t, resp))
	resp = f.redeemInvite(strings.Repeat("A", 43))
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, CodeSignInDisabled, errCode(t, resp))
}

func TestInvite_BrowserRules(t *testing.T) {
	f := newCeremonyFixture(t)
	secret := secretOf(t, f.inviteLink("Bob").URL)

	resp := f.redeemInvite(secret, withoutHeader(HeaderCSRF))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "CSRF_HEADER_REQUIRED", errCode(t, resp))

	resp = f.redeemInvite(secret, withHeader("Sec-Fetch-Site", "cross-site"))
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.Equal(t, "CROSS_ORIGIN_REQUEST", errCode(t, resp))

	resp = f.redeemInvite(secret)
	assert.Equal(t, http.StatusOK, resp.StatusCode, "the refusals spent nothing")
}

func TestSignInLink_MintRedeemFreshIdentity(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.signInLink(f.ops, "other")
	require.Equal(t, http.StatusCreated, resp.StatusCode, errBody(t, resp))
	link := decodeBody[linkResponse](t, resp)
	assert.True(t, strings.HasPrefix(link.URL, publicURL+SignInPath+"#t="), "link %q", link.URL)
	assert.Equal(t, int(authn.MagicLinkTTL.Seconds()), link.ExpiresIn)

	resp = f.redeemLink(secretOf(t, link.URL))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	c := sessionCookieOf(t, resp)
	signed := decodeBody[exchangeResponse](t, resp)
	assert.Equal(t, "other", signed.Principal)
	assert.Equal(t, []string{authn.RoleReader}, signed.Roles)

	store := f.bundle.svc.Store(ownerCtx()).Auth()
	sess, err := f.sessions.Get(ownerCtx(), signed.Session.ID)
	require.NoError(t, err)
	idn, err := store.Identities().Get(ownerCtx(), sess.IdentityID)
	require.NoError(t, err)
	assert.Equal(t, storage.IdentityMethodLink, idn.Method)
	assert.Equal(t, authn.LinkIdentitySignIn, idn.Name)

	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp = f.redeemLink(secretOf(t, link.URL))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "single use")

	// Revoking the link identity ends the session; the person's key
	// still works.
	resp = f.do(http.MethodDelete, "/api/v1/principals/other/identities/"+idn.ID, nil, withBearer(f.ops))
	require.Equal(t, http.StatusNoContent, resp.StatusCode, errBody(t, resp))
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withCookie(c), fromUI)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	resp = f.do(http.MethodGet, "/api/v1/whoami", nil, withBearer(f.other))
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSignInLink_RefusedForSuspendedOrRevokedPrincipal(t *testing.T) {
	f := newCeremonyFixture(t)
	store := f.bundle.svc.Store(ownerCtx()).Auth()

	resp := f.signInLink(f.ops, "prn_nobody")
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Minted while active, redeemed after suspension.
	resp = f.signInLink(f.ops, "other")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	link := decodeBody[linkResponse](t, resp)
	resp = f.do(http.MethodPost, "/api/v1/principals/other/suspend", nil, withBearer(f.ops))
	require.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
	resp = f.redeemLink(secretOf(t, link.URL))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, CodeInvalidTicket, errCode(t, resp), "the refusal says nothing about the principal")

	resp = f.signInLink(f.ops, "other")
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, CodePrincipalInactive, errCode(t, resp))

	require.NoError(t, store.Principals().Revoke(ownerCtx(), "other", f.clock.now()))
	resp = f.signInLink(f.ops, "other")
	assert.Equal(t, http.StatusConflict, resp.StatusCode)

	resp = f.signInLink(f.other, storage.OwnerPrincipal)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "a revoked principal's key is refused")
}

func TestSignInLink_Expires(t *testing.T) {
	f := newCeremonyFixture(t)
	resp := f.signInLink(f.ops, "other")
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	link := decodeBody[linkResponse](t, resp)
	f.clock.advance(authn.MagicLinkTTL)
	resp = f.redeemLink(secretOf(t, link.URL))
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestCeremonies_RateLimitedPerAddress(t *testing.T) {
	f := newCeremonyFixture(t)
	bogus := strings.Repeat("A", 43)

	// Every ceremony shares one bucket per address: N attempts pass
	// (as refusals), the N+1th is 429 with a Retry-After.
	calls := []func() *http.Response{
		func() *http.Response { return f.redeemInvite(bogus) },
		func() *http.Response { return f.redeemLink(bogus) },
		func() *http.Response { return f.exchange(bogus) },
	}
	for i := range ceremonyAttempts {
		resp := calls[i%len(calls)]()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "attempt %d", i+1)
	}
	resp := f.redeemInvite(bogus)
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, CodeRateLimited, errCode(t, resp))
	assert.NotEmpty(t, resp.Header.Get("Retry-After"))

	// A refill lets the next attempt through; a valid ticket then
	// redeems as usual.
	f.clock.advance(time.Minute)
	secret := secretOf(t, f.inviteLink("Bob").URL)
	resp = f.redeemInvite(secret)
	assert.Equal(t, http.StatusOK, resp.StatusCode, errBody(t, resp))
}

func TestAddrLimiter(t *testing.T) {
	c := &testClock{t: time.Now()}
	l := NewAddrLimiter(2, time.Minute, c.now)
	ok, _ := l.Allow("a")
	assert.True(t, ok)
	ok, _ = l.Allow("a")
	assert.True(t, ok)
	ok, retry := l.Allow("a")
	assert.False(t, ok)
	assert.InDelta(t, 30*time.Second, retry, float64(time.Second), "one token refills in half the window")
	ok, _ = l.Allow("b")
	assert.True(t, ok, "another address has its own bucket")
	c.advance(30 * time.Second)
	ok, _ = l.Allow("a")
	assert.True(t, ok, "refilled")
	ok, _ = l.Allow("a")
	assert.False(t, ok)
	c.advance(10 * time.Minute)
	ok, _ = l.Allow("a")
	assert.True(t, ok, "a full bucket after the sweep")
}
