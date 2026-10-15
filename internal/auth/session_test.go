package auth_test

import (
	"context"
	"strings"
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

// fixture is a principal registry with the owner (admin) and a person
// (reader), one API key each, sessions over the same store and the
// api_key and session methods over both.
type fixture struct {
	t        *testing.T
	drv      storage.StorageDriver
	clock    *clock
	keys     *auth.Keys
	sessions *auth.Sessions
	method   *auth.APIKeyMethod
	person   *storage.Principal
	// ownerKey and personKey are the minted secrets.
	ownerKey, personKey string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, drv: storageutil.NewTestDriver(t), clock: newClock()}
	ctx := context.Background()
	f.keys = auth.NewKeys(auth.NewLocalKeys(f.drv.Auth().KV(), f.clock.now), f.drv.Auth(), f.clock.now)
	f.person = &storage.Principal{ID: "prn_person", Kind: storage.PrincipalKindPerson, DisplayName: "Alice",
		Roles: []string{auth.RoleReader}, CreatedAt: f.clock.t}
	require.NoError(t, f.drv.Auth().Principals().Create(ctx, f.person))
	f.ownerKey = f.mint(storage.OwnerPrincipal, "laptop", nil)
	f.personKey = f.mint(f.person.ID, "phone", nil)
	var err error
	f.sessions, err = auth.NewSessions(f.drv.UISessions(), f.drv.Auth(), f.keys, auth.SessionOptions{Now: f.clock.now})
	require.NoError(t, err)
	f.method = auth.NewAPIKeyMethod(f.keys, nil)
	return f
}

// mint issues a key for principal with scopes (nil = the role scopes).
func (f *fixture) mint(principal, name string, scopes []auth.Scope) string {
	f.t.Helper()
	secret, _, err := f.keys.Mint(context.Background(), auth.MintKey{PrincipalID: principal, Name: name, Scopes: scopes})
	require.NoError(f.t, err)
	return secret
}

// principalOf authenticates key through the api_key method.
func (f *fixture) principalOf(key string) *auth.Principal {
	f.t.Helper()
	p, ok, err := f.method.Authenticate(context.Background(), auth.Credential{Scheme: auth.SchemeBearer, Token: key})
	require.NoError(f.t, err)
	require.True(f.t, ok)
	return p
}

// signIn mints a login code with key and exchanges it.
func (f *fixture) signIn(key string) *auth.Minted {
	f.t.Helper()
	code, _, err := f.sessions.MintCode(context.Background(), f.principalOf(key))
	require.NoError(f.t, err)
	m, err := f.sessions.Exchange(context.Background(), code, auth.ClientInfo{UserAgent: "test", RemoteAddr: "127.0.0.1"})
	require.NoError(f.t, err)
	return m
}

func TestSessions_ExchangeActsAsMintingPrincipal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.principalOf(f.ownerKey)
	code, expires, err := f.sessions.MintCode(ctx, owner)
	require.NoError(t, err)
	assert.Len(t, code, 43, "256-bit base64url code")
	assert.Equal(t, f.clock.t.Add(auth.DefaultLoginCodeTTL), expires)

	m, err := f.sessions.Exchange(ctx, code, auth.ClientInfo{UserAgent: "ua", RemoteAddr: "192.0.2.7"})
	require.NoError(t, err)
	assert.NotEqual(t, code, m.Secret)
	assert.Equal(t, storage.OwnerPrincipal, m.Principal.ID)
	assert.Equal(t, storage.PrincipalKindOwner, m.Principal.Kind)
	assert.Equal(t, auth.MethodSession, m.Principal.Provider)
	assert.Equal(t, []string{auth.RoleAdmin}, m.Principal.Roles)
	assert.True(t, m.Principal.IsSession())
	assert.Equal(t, auth.SessionKindUI, m.Principal.SessionKind())
	assert.Equal(t, auth.UISessionScopes, m.Principal.Scopes, "an admin key's session holds the ui set, no more")
	assert.Equal(t, m.Session.ID, m.Principal.Meta[auth.MetaSessionID])
	assert.Equal(t, owner.Meta[auth.MetaIdentityID], m.Session.IdentityID, "the session records the identity that minted it")
	assert.Equal(t, m.Session.IdentityID, m.Principal.Meta[auth.MetaIdentityID])
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionIdleTTL), m.Session.IdleExpiresAt)
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionMaxTTL), m.Session.ExpiresAt)

	stored, err := f.drv.UISessions().Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, auth.HashSecret(m.Secret), stored.SecretHash, "only the secret's hash is stored")

	p, sess, err := f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	assert.Equal(t, storage.OwnerPrincipal, p.ID)
	assert.Equal(t, m.Session.ID, sess.ID)
}

func TestSessions_CodeIsSingleUse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, f.principalOf(f.ownerKey))
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "replayed code")
}

func TestSessions_CodeExpires(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, f.principalOf(f.ownerKey))
	require.NoError(t, err)
	f.clock.advance(auth.DefaultLoginCodeTTL)
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_WrongOrMissingCode(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	code, _, err := f.sessions.MintCode(ctx, f.principalOf(f.ownerKey))
	require.NoError(t, err)
	_, err = f.sessions.Exchange(ctx, code[:len(code)-1]+"x", auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, err = f.sessions.Exchange(ctx, "", auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrNoCredential)
	// The near miss did not spend the real code.
	_, err = f.sessions.Exchange(ctx, code, auth.ClientInfo{})
	assert.NoError(t, err)
}

// Only a registry key, with an identity row to bind the session to, may
// mint: a key declared in config has none, and a session never mints
// another.
func TestSessions_MintNeedsAnIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_, _, err := f.sessions.MintCode(ctx, nil)
	assert.ErrorIs(t, err, auth.ErrNoCredential)

	cfgKey := "config-declared-key-0123456789abcdef0123456789"
	cfg, err := auth.ConfigKeysFrom(configEntries(cfgKey, storage.OwnerPrincipal, nil, ""))
	require.NoError(t, err)
	p, ok, err := auth.NewAPIKeyMethod(f.keys, cfg).Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: cfgKey})
	require.NoError(t, err)
	require.True(t, ok)
	_, _, err = f.sessions.MintCode(ctx, p)
	assert.ErrorIs(t, err, auth.ErrNoIdentity)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)

	m := f.signIn(f.ownerKey)
	_, _, err = f.sessions.MintCode(ctx, m.Principal)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "a session cannot mint a session")
}

func TestSessions_IdleExpiry(t *testing.T) {
	f := newFixture(t)
	m := f.signIn(f.ownerKey)
	f.clock.advance(auth.DefaultSessionIdleTTL)
	_, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}

func TestSessions_ActivityExtendsUntilMaxTTL(t *testing.T) {
	f := newFixture(t)
	m := f.signIn(f.ownerKey)
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
	f := newFixture(t)
	m := f.signIn(f.ownerKey)
	ctx := context.Background()
	f.clock.advance(auth.DefaultSessionTouchEvery / 2)
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	got, err := f.drv.UISessions().Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, m.Session.LastSeenAt, got.LastSeenAt, "no write inside the touch interval")

	f.clock.advance(auth.DefaultSessionTouchEvery)
	_, _, err = f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	got, err = f.drv.UISessions().Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, f.clock.t, got.LastSeenAt)
	assert.Equal(t, f.clock.t.Add(auth.DefaultSessionIdleTTL), got.IdleExpiresAt)
}

func TestSessions_Revoke(t *testing.T) {
	f := newFixture(t)
	m := f.signIn(f.ownerKey)
	ctx := context.Background()
	require.NoError(t, f.sessions.Revoke(ctx, m.Session.ID, auth.RevokeReasonLogout))
	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	assert.ErrorIs(t, f.sessions.Revoke(ctx, "uis_nope", auth.RevokeReasonRevoked), storage.ErrNotFound)
}

func TestSessions_UnknownSecret(t *testing.T) {
	f := newFixture(t)
	_, _, err := f.sessions.Authenticate(context.Background(), "not-a-session")
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	_, _, err = f.sessions.Authenticate(context.Background(), "")
	assert.ErrorIs(t, err, auth.ErrNoCredential)
}

// Revoking the key that signed a browser in ends that session at once,
// through the store's cascade, and a code it minted can no longer be
// exchanged; a session of the principal's other key lives on.
func TestSessions_RevokedIdentityEndsItsSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	second := f.mint(storage.OwnerPrincipal, "desktop", nil)
	first, other := f.signIn(f.ownerKey), f.signIn(second)
	pending, _, err := f.sessions.MintCode(ctx, f.principalOf(f.ownerKey))
	require.NoError(t, err)

	require.NoError(t, f.keys.Revoke(ctx, f.principalOf(second).Meta[auth.MetaKeyID]))
	_, _, err = f.sessions.Authenticate(ctx, other.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	got, err := f.drv.UISessions().Get(ctx, other.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.RevokeReasonIdentityRevoked, got.RevokeReason)
	_, _, err = f.sessions.Authenticate(ctx, first.Secret)
	assert.NoError(t, err, "the other key's session is untouched")

	require.NoError(t, f.keys.Revoke(ctx, f.principalOf(f.ownerKey).Meta[auth.MetaKeyID]))
	_, err = f.sessions.Exchange(ctx, pending, auth.ClientInfo{})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "a code of a revoked key cannot be exchanged")
}

// Revoking a principal ends every session of it, and its key stops
// verifying, in one step.
func TestSessions_RevokedPrincipalEndsEverything(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.signIn(f.personKey)
	require.NoError(t, f.drv.Auth().Principals().Revoke(ctx, f.person.ID, f.clock.t))

	_, _, err := f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	got, err := f.drv.UISessions().Get(ctx, m.Session.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.RevokeReasonPrincipalRevoked, got.RevokeReason)
	_, ok, err := f.method.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: f.personKey})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	assert.False(t, ok)
}

// A session bound to a non-key identity narrows the principal's role
// scopes, and another principal's revocation leaves it alone.
func TestSessions_NonKeyIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.signIn(f.ownerKey)
	idn := &storage.Identity{PrincipalID: storage.OwnerPrincipal, Method: storage.IdentityMethodPasskey, Subject: "cred-1"}
	require.NoError(t, f.drv.Auth().Identities().Create(ctx, idn))
	now := f.clock.t
	require.NoError(t, f.drv.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_passkey", SecretHash: auth.HashSecret(strings.Repeat("C", 43)), PrincipalID: storage.OwnerPrincipal,
		IdentityID: idn.ID, Scope: auth.SessionKindUI,
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour),
	}))
	p, _, err := f.sessions.Authenticate(ctx, strings.Repeat("C", 43))
	require.NoError(t, err)
	assert.Equal(t, auth.UISessionScopes, p.Scopes, "a non-key identity narrows the principal's role scopes")

	require.NoError(t, f.drv.Auth().Principals().Revoke(ctx, f.person.ID, now))
	_, _, err = f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err, "the owner's session is untouched by another principal's revocation")
	_, _, err = f.sessions.Authenticate(ctx, strings.Repeat("C", 43))
	require.NoError(t, err)
}

// A session's effective scopes are the minting key's intersected with
// the ui set: a reader's session reads only, a writer's may also retry
// jobs, an admin's holds exactly the ui set, and a key narrowed below
// its role narrows its session with it. None may mint login codes.
func TestSessions_ScopesIntersectKeyAndUISet(t *testing.T) {
	uiRead := []auth.Scope{
		auth.ScopeReadObjects, auth.ScopeReadInbox, auth.ScopeReadFeeds, auth.ScopeReadJobs,
		auth.ScopeReadRegistries, auth.ScopeReadSystem,
	}
	cases := map[string][]auth.Scope{
		auth.RoleReader: append(append([]auth.Scope{}, uiRead...), auth.ScopeWriteSelf, auth.ScopeSignoutUI),
		auth.RoleWriter: {
			auth.ScopeReadObjects, auth.ScopeReadInbox, auth.ScopeReadFeeds, auth.ScopeReadJobs, auth.ScopeWriteJobs,
			auth.ScopeReadRegistries, auth.ScopeReadSystem, auth.ScopeWriteSelf, auth.ScopeSignoutUI,
		},
		auth.RoleAdmin: auth.UISessionScopes,
	}
	for role, want := range cases {
		t.Run(role, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			p := &storage.Principal{ID: "prn_" + role, Kind: storage.PrincipalKindPerson, DisplayName: role, Roles: []string{role}}
			require.NoError(t, f.drv.Auth().Principals().Create(ctx, p))
			key := f.mint(p.ID, "k", nil)
			m := f.signIn(key)
			assert.Equal(t, want, m.Principal.Scopes)
			sp, _, err := f.sessions.Authenticate(ctx, m.Secret)
			require.NoError(t, err)
			assert.Equal(t, want, sp.Scopes)
			kp := f.principalOf(key)
			for _, s := range sp.Scopes {
				if s == auth.ScopeSignoutUI {
					continue // session-only: no key holds it
				}
				assert.True(t, kp.HasScope(s), "session holds %s its key lacks", s)
			}
			assert.False(t, sp.HasScope(auth.ScopeReadUI), "a session never mints login codes")
			assert.True(t, kp.HasScope(auth.ScopeReadUI), "every role may sign a browser in")
			assert.True(t, sp.HasScope(auth.ScopeSignoutUI), "every session may sign itself out")
			assert.False(t, kp.HasScope(auth.ScopeSignoutUI), "signout:ui is session-only")
		})
	}
	t.Run("narrowed key", func(t *testing.T) {
		f := newFixture(t)
		key := f.mint(storage.OwnerPrincipal, "narrow", []auth.Scope{auth.ScopeReadObjects, auth.ScopeReadUI})
		m := f.signIn(key)
		assert.Equal(t, []auth.Scope{auth.ScopeReadObjects, auth.ScopeSignoutUI}, m.Principal.Scopes,
			"an owner's session through a narrowed key holds only what the key holds")
	})
}

func TestSessionPrincipal_MetaIsolated(t *testing.T) {
	f := newFixture(t)
	m := f.signIn(f.ownerKey)
	m.Principal.Meta[auth.MetaSessionKind] = "full"
	m.Principal.Scopes[0] = auth.ScopeAdminAudit
	p, _, err := f.sessions.Authenticate(context.Background(), m.Secret)
	require.NoError(t, err)
	assert.Equal(t, auth.SessionKindUI, p.SessionKind())
	assert.Equal(t, auth.UISessionScopes, p.Scopes)
}

// A stored session of a kind this build does not know reaches nothing.
func TestSessions_UnknownKindHasNoScopes(t *testing.T) {
	f := newFixture(t)
	now := f.clock.t
	owner := f.principalOf(f.ownerKey)
	require.NoError(t, f.drv.UISessions().Create(context.Background(), &storage.UISession{
		ID: "uis_future", SecretHash: auth.HashSecret("secret-future"), PrincipalID: storage.OwnerPrincipal,
		IdentityID: owner.Meta[auth.MetaIdentityID], Scope: "full",
		CreatedAt: now, LastSeenAt: now, IdleExpiresAt: now.Add(time.Hour), ExpiresAt: now.Add(2 * time.Hour),
	}))
	p, _, err := f.sessions.Authenticate(context.Background(), "secret-future")
	require.NoError(t, err)
	assert.Empty(t, p.Scopes)
}

// The session method is ambient; no cookie is absent, a cookie of the
// wrong shape is refused, an issued but dead cookie is absent so the
// chain clears it, a live one matches.
func TestSessionMethod(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.sessions.Method()
	assert.Equal(t, auth.MethodSession, m.Name())
	assert.True(t, m.Ambient())

	p, ok, err := m.Authenticate(ctx, auth.Credential{})
	assert.Nil(t, p)
	assert.False(t, ok)
	assert.NoError(t, err)

	_, ok, err = m.Authenticate(ctx, auth.Credential{Cookie: "nope"})
	assert.False(t, ok)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential, "a cookie this server never issued fails its integrity check")

	_, ok, err = m.Authenticate(ctx, auth.Credential{Cookie: strings.Repeat("A", 43)})
	assert.False(t, ok)
	assert.NoError(t, err, "a well-formed unknown cookie is absent, not refused")

	live := f.signIn(f.ownerKey)
	p, ok, err = m.Authenticate(ctx, auth.Credential{Cookie: live.Secret})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, p.IsSession())

	f.clock.advance(auth.DefaultSessionIdleTTL)
	_, ok, err = m.Authenticate(ctx, auth.Credential{Cookie: live.Secret})
	assert.False(t, ok)
	assert.NoError(t, err, "an expired cookie is absent")

	// A bearer header is not a cookie and a cookie is not a bearer.
	_, ok, err = m.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: live.Secret})
	assert.False(t, ok)
	assert.NoError(t, err)
	_, ok, err = f.method.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: live.Secret})
	assert.False(t, ok)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
}
