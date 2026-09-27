package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// clock is a settable time source for session tests.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock                   { return &clock{t: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)} }
func staticTokens(t *testing.T, toks ...auth.StaticToken) *auth.StaticProvider {
	t.Helper()
	p, err := auth.NewStatic(toks)
	require.NoError(t, err)
	return p
}

var (
	opsToken   = auth.StaticToken{Token: "tok-ops", Principal: "ops", Roles: []string{"admin", "reader"}}
	otherToken = auth.StaticToken{Token: "tok-other", Principal: "other", Roles: []string{"reader"}}
)

type sessionFixture struct {
	store    storage.UISessionStore
	clock    *clock
	static   *auth.StaticProvider
	sessions *auth.Sessions
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	f := &sessionFixture{store: storageutil.NewTestDriver(t).UISessions(), clock: newClock()}
	f.restart(t, opsToken, otherToken)
	return f
}

// restart rebuilds the provider and session manager from a new token
// table over the same store: what a dpkms restart with edited config does.
func (f *sessionFixture) restart(t *testing.T, toks ...auth.StaticToken) {
	t.Helper()
	f.static = staticTokens(t, toks...)
	s, err := auth.NewSessions(f.store, f.static, auth.SessionOptions{Now: f.clock.now})
	require.NoError(t, err)
	f.sessions = s
}

func (f *sessionFixture) signIn(t *testing.T) *auth.Minted {
	t.Helper()
	code, _, err := f.sessions.MintCode(context.Background(), opsToken.Token)
	require.NoError(t, err)
	m, err := f.sessions.Exchange(context.Background(), code, auth.ClientInfo{UserAgent: "test", RemoteAddr: "127.0.0.1"})
	require.NoError(t, err)
	return m
}

func TestSessions_ExchangeActsAsMintingPrincipal(t *testing.T) {
	f := newSessionFixture(t)
	ctx := context.Background()
	code, expires, err := f.sessions.MintCode(ctx, opsToken.Token)
	require.NoError(t, err)
	assert.Len(t, code, 43, "256-bit base64url code")
	assert.Equal(t, f.clock.t.Add(auth.DefaultLoginCodeTTL), expires)

	m, err := f.sessions.Exchange(ctx, code, auth.ClientInfo{UserAgent: "ua", RemoteAddr: "192.0.2.7"})
	require.NoError(t, err)
	assert.NotEqual(t, code, m.Secret)
	assert.Equal(t, "ops", m.Principal.ID)
	assert.Equal(t, []string{"admin", "reader"}, m.Principal.Roles)
	assert.True(t, m.Principal.IsSession())
	assert.Equal(t, auth.SessionKindUI, m.Principal.SessionKind())
	assert.Equal(t, auth.UISessionScopes, m.Principal.Scopes, "an admin token's session holds the ui set, no more")
	assert.Equal(t, m.Session.ID, m.Principal.Meta[auth.MetaSessionID])
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionIdleTTL), m.Session.IdleExpiresAt)
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionMaxTTL), m.Session.ExpiresAt)
	assert.Equal(t, auth.HashSecret(opsToken.Token), m.Session.TokenHash)

	stored, err := f.store.Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.HashSecret(m.Secret), stored.SecretHash, "only the secret's hash is stored")

	p, sess, err := f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	assert.Equal(t, "ops", p.ID)
	assert.Equal(t, m.Session.ID, sess.ID)
}

func TestSessions_CodeIsSingleUse(t *testing.T) {
	f := newSessionFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, opsToken.Token)
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "replayed code")
}

func TestSessions_CodeExpires(t *testing.T) {
	f := newSessionFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, opsToken.Token)
	require.NoError(t, err)
	f.clock.advance(auth.DefaultLoginCodeTTL)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_WrongOrMissingCode(t *testing.T) {
	f := newSessionFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, opsToken.Token)
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code[:len(code)-1]+"x", auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, err = f.sessions.Exchange(ctx, "", auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrNoCredential)
	// The near miss did not spend the real code.
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.NoError(t, err)
}

func TestSessions_MintNeedsConfiguredToken(t *testing.T) {
	f := newSessionFixture(t)
	_, _, err := f.sessions.MintCode(context.Background(), "tok-unknown")
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, _, err = f.sessions.MintCode(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrNoCredential)
}

func TestSessions_IdleExpiry(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	f.clock.advance(auth.DefaultSessionIdleTTL)
	_, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_ActivityExtendsUntilMaxTTL(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	ctx := context.Background()
	step := auth.DefaultSessionIdleTTL - time.Hour
	elapsed := time.Duration(0)
	for elapsed+step < auth.DefaultSessionMaxTTL {
		f.clock.advance(step)
		elapsed += step
		_, _, err := f.sessions.Authenticate(ctx, m.Secret)
		require.NoError(t, err, "active session refused after %s", elapsed)
	}
	f.clock.t = m.Session.ExpiresAt
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "max TTL ends even an active session")
}

func TestSessions_TouchIsThrottled(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	ctx := context.Background()
	f.clock.advance(auth.DefaultSessionTouchEvery / 2)
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	got, err := f.store.Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, m.Session.LastSeenAt, got.LastSeenAt, "no write inside the touch interval")

	f.clock.advance(auth.DefaultSessionTouchEvery)
	_, _, err = f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	got, err = f.store.Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, f.clock.t, got.LastSeenAt)
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionIdleTTL), got.IdleExpiresAt)
}

func TestSessions_Revoke(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	ctx := context.Background()
	require.NoError(t, f.sessions.Revoke(ctx, m.Session.ID, auth.RevokeReasonLogout))
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	assert.ErrorIs(t, f.sessions.Revoke(ctx, "uis_nope", auth.RevokeReasonRevoked), storage.ErrNotFound)
}

func TestSessions_UnknownSecret(t *testing.T) {
	f := newSessionFixture(t)
	_, _, err := f.sessions.Authenticate(context.Background(), "not-a-session")
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, _, err = f.sessions.Authenticate(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrNoCredential)
}

func TestSessions_TokenRemovedFromConfigEndsSession(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	ctx := context.Background()

	f.restart(t, otherToken) // ops token removed from config
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	got, err := f.store.Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.RevokedAt)
	assert.Equal(t, auth.RevokeReasonTokenRemoved, got.RevokeReason)

	f.restart(t, opsToken, otherToken) // restoring the token does not revive it
	_, _, err = f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_TokenReassignedEndsSession(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	f.restart(t, auth.StaticToken{Token: opsToken.Token, Principal: "someone-else", Roles: []string{"admin"}})
	_, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_ExchangeAfterTokenRemoved(t *testing.T) {
	f := newSessionFixture(t)
	code, _, err := f.sessions.MintCode(context.Background(), opsToken.Token)
	require.NoError(t, err)
	f.restart(t, otherToken)
	_, err = f.sessions.Exchange(context.Background(), code, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_RolesFollowConfig(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	f.restart(t, auth.StaticToken{Token: opsToken.Token, Principal: "ops", Roles: []string{"reader"}})
	p, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	require.NoError(t, err)
	assert.Equal(t, []string{"reader"}, p.Roles)
	assert.Equal(t, auth.SessionScopesFor(auth.ScopesForRoles([]string{"reader"}), auth.SessionKindUI), p.Scopes)
}

func TestSessions_SweepRemovedTokens(t *testing.T) {
	f := newSessionFixture(t)
	gone := f.signIn(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, otherToken.Token)
	require.NoError(t, err)
	kept, err := f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	require.NoError(t, err)

	f.restart(t, otherToken)
	n, err := f.sessions.SweepRemovedTokens(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got, err := f.store.Get(ctx, gone.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.RevokeReasonTokenRemoved, got.RevokeReason)
	_, _, err = f.sessions.Authenticate(ctx, kept.Secret)
	assert.NoError(t, err)
}

func TestSessionProvider_BearerUnchangedCookieWorks(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	p := f.sessions.Provider(f.static)
	ctx := context.Background()

	assert.Equal(t, auth.ProviderStatic, p.Name())

	princ, err := p.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: opsToken.Token})
	require.NoError(t, err)
	assert.Equal(t, "ops", princ.ID)
	assert.False(t, princ.IsSession(), "a bearer principal is not a session")

	princ, err = p.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeSession, Token: m.Secret})
	require.NoError(t, err)
	assert.True(t, princ.IsSession())

	// The cookie secret is not a bearer token, and a bearer token is not
	// a cookie.
	_, err = p.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: m.Secret})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, err = p.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeSession, Token: opsToken.Token})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, err = p.Authenticate(ctx, auth.Credential{})
	assert.True(t, errors.Is(err, auth.ErrNoCredential))
}

func TestStatic_RefusesSessionScheme(t *testing.T) {
	p := staticTokens(t, opsToken)
	_, err := p.Authenticate(context.Background(), auth.Credential{Scheme: auth.SchemeSession, Token: opsToken.Token})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestStatic_PrincipalForTokenHash(t *testing.T) {
	p := staticTokens(t, opsToken, otherToken)
	got, ok := p.PrincipalForTokenHash(auth.HashSecret(otherToken.Token))
	require.True(t, ok)
	assert.Equal(t, "other", got.ID)
	_, ok = p.PrincipalForTokenHash(auth.HashSecret("tok-nope"))
	assert.False(t, ok)
	_, ok = p.PrincipalForTokenHash(otherToken.Token)
	assert.False(t, ok, "the raw token is not its hash")
}

func TestSessionPrincipal_MetaIsolated(t *testing.T) {
	f := newSessionFixture(t)
	m := f.signIn(t)
	m.Principal.Meta[auth.MetaSessionKind] = "full"
	m.Principal.Scopes[0] = auth.ScopeAdminAudit
	p, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	require.NoError(t, err)
	assert.Equal(t, auth.SessionKindUI, p.SessionKind())
	assert.Equal(t, auth.UISessionScopes, p.Scopes)
}

// A session's effective scopes are its minting principal's intersected
// with the ui set: a reader token's session reads only, a writer's may
// also retry jobs, an admin's holds exactly the ui set. None may mint
// login codes, whatever the token.
func TestSessions_ScopesIntersectTokenAndUISet(t *testing.T) {
	uiRead := []auth.Scope{
		auth.ScopeReadObjects, auth.ScopeReadInbox, auth.ScopeReadFeeds, auth.ScopeReadJobs,
		auth.ScopeReadRegistries, auth.ScopeReadSystem,
	}
	cases := map[string][]auth.Scope{
		auth.RoleReader: append(append([]auth.Scope{}, uiRead...), auth.ScopeSignoutUI),
		auth.RoleWriter: {
			auth.ScopeReadObjects, auth.ScopeReadInbox, auth.ScopeReadFeeds, auth.ScopeReadJobs, auth.ScopeWriteJobs,
			auth.ScopeReadRegistries, auth.ScopeReadSystem, auth.ScopeSignoutUI,
		},
		auth.RoleAdmin: auth.UISessionScopes,
	}
	for role, want := range cases {
		t.Run(role, func(t *testing.T) {
			f := newSessionFixture(t)
			f.restart(t, auth.StaticToken{Token: opsToken.Token, Principal: "ops", Roles: []string{role}})
			m := f.signIn(t)
			assert.Equal(t, want, m.Principal.Scopes)
			p, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
			require.NoError(t, err)
			assert.Equal(t, want, p.Scopes)
			token, err := f.static.Authenticate(context.Background(), auth.Credential{Scheme: auth.SchemeBearer, Token: opsToken.Token})
			require.NoError(t, err)
			for _, s := range p.Scopes {
				if s == auth.ScopeSignoutUI {
					continue // session-only: no token holds it
				}
				assert.True(t, token.HasScope(s), "session holds %s its token lacks", s)
			}
			assert.False(t, p.HasScope(auth.ScopeReadUI), "a session never mints login codes")
			assert.True(t, token.HasScope(auth.ScopeReadUI), "every role may sign a browser in")
			assert.True(t, p.HasScope(auth.ScopeSignoutUI), "every session may sign itself out")
			assert.False(t, token.HasScope(auth.ScopeSignoutUI), "signout:ui is session-only")
		})
	}
}

// A stored session of a kind this build does not know reaches nothing.
func TestSessions_UnknownKindHasNoScopes(t *testing.T) {
	f := newSessionFixture(t)
	now := f.clock.t
	require.NoError(t, f.store.Create(context.Background(), &storage.UISession{
		ID: "uis_future", SecretHash: auth.HashSecret("secret-future"), PrincipalID: "ops",
		TokenHash: auth.HashSecret(opsToken.Token), Scope: "full",
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour),
	}))
	p, _, err := f.sessions.Authenticate(context.Background(), "secret-future")
	require.NoError(t, err)
	assert.Empty(t, p.Scopes)
}
