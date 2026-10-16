package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// configEntries is one server.auth.keys entry for the SHA-256 of key.
func configEntries(key, principal string, roles []string, expires string) []config.AuthKeyConfig {
	return []config.AuthKeyConfig{{Hash: "sha256:" + auth.HashSecret(key), Principal: principal, Roles: roles, Expires: expires}}
}

// chainStates holds one credential per state the table runs through.
type chainStates struct {
	keys    map[string]string // key state → bearer token ("" = absent)
	cookies map[string]string // cookie state → cookie value ("" = absent)
}

// newChainStates mints every key and session state against f: an
// expired key (issued with a short TTL before the clock moved), a valid
// one, a revoked one, a wrong one and a malformed one; a valid session
// of the person, an expired session, a well-formed unknown cookie and a
// malformed one.
func newChainStates(f *fixture) chainStates {
	ctx := context.Background()
	expired, _, err := f.keys.Mint(ctx, auth.MintKey{PrincipalID: storage.OwnerPrincipal, Name: "expired", ExpiresAt: f.clock.t.Add(time.Minute)})
	require.NoError(f.t, err)
	f.clock.advance(2 * time.Minute)
	revoked, issued, err := f.keys.Mint(ctx, auth.MintKey{PrincipalID: storage.OwnerPrincipal, Name: "revoked"})
	require.NoError(f.t, err)
	require.NoError(f.t, f.keys.Revoke(ctx, issued.ID))
	wrong := expired[:len(expired)-1] + "x"
	if wrong == expired {
		wrong = expired[:len(expired)-1] + "y"
	}

	live := f.signIn(f.personKey)
	now := f.clock.t
	require.NoError(f.t, f.drv.UISessions().Create(ctx, &storage.UISession{
		ID: "uis_expired", SecretHash: auth.HashSecret(strings.Repeat("A", 43)), PrincipalID: f.person.ID,
		IdentityID: live.Session.IdentityID, Scope: auth.SessionKindUI,
		CreatedAt: now.Add(-2 * time.Hour), LastSeenAt: now.Add(-2 * time.Hour),
		IdleExpiresAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour),
	}))
	return chainStates{
		keys: map[string]string{
			"absent": "", "valid": f.ownerKey, "expired": expired, "revoked": revoked, "wrong": wrong, "malformed": "nope",
		},
		cookies: map[string]string{
			"absent": "", "valid": live.Secret, "expired": strings.Repeat("A", 43), "unknown": strings.Repeat("B", 43), "malformed": "nope",
		},
	}
}

// want is the outcome of one table cell.
type want struct {
	principal string // "" = no principal
	method    string
	ambient   bool
	err       error // nil, auth.ErrNoCredential or auth.ErrInvalidCredential
	clear     bool
}

// expect is the chain contract (ADR-079 §3) for a key state, a cookie
// state and whether the session method runs: a carried credential is
// decided first and alone; an ambient one only counts when nothing was
// carried; a dead cookie is absent and cleared; only a cookie that
// fails its integrity check is refused.
func expect(key, cookie string, session bool, owner, person string) want {
	switch key {
	case "valid":
		return want{principal: owner, method: auth.MethodAPIKey}
	case "expired", "revoked", "wrong", "malformed":
		return want{err: auth.ErrInvalidCredential}
	}
	// No key: the cookie decides.
	if !session {
		return want{err: auth.ErrNoCredential, clear: cookie != "absent"}
	}
	switch cookie {
	case "valid":
		return want{principal: person, method: auth.MethodSession, ambient: true}
	case "expired", "unknown":
		return want{err: auth.ErrNoCredential, clear: true}
	case "malformed":
		return want{err: auth.ErrInvalidCredential}
	}
	return want{err: auth.ErrNoCredential}
}

// TestChain_Table runs every combination of key and cookie state through
// the chain in every configured order: a valid key wins over any cookie
// (an expired cookie then a valid key is the key), a bad key is refused
// whatever cookie stands behind it (an expired key with a valid cookie
// is 401), and a cookie only counts when no key was carried. The
// configured order never changes the outcome: carried runs first.
func TestChain_Table(t *testing.T) {
	f := newFixture(t)
	st := newChainStates(f)
	ctx := context.Background()
	for _, order := range []struct {
		name    string
		methods []string
	}{
		{"api_key then session", []string{auth.MethodAPIKey, auth.MethodSession}},
		{"session then api_key", []string{auth.MethodSession, auth.MethodAPIKey}},
		{"api_key only", []string{auth.MethodAPIKey}},
	} {
		chain, err := auth.ChainFor(order.methods, f.method, f.sessions.Method())
		require.NoError(t, err)
		hasSession := chain.Has(auth.MethodSession)
		for keyState, key := range st.keys {
			for cookieState, cookie := range st.cookies {
				t.Run(order.name+"/key="+keyState+"/cookie="+cookieState, func(t *testing.T) {
					cred := auth.Credential{Cookie: cookie}
					if key != "" {
						cred.Scheme, cred.Token = auth.SchemeBearer, key
					}
					w := expect(keyState, cookieState, hasSession, storage.OwnerPrincipal, f.person.ID)
					res, err := chain.Authenticate(ctx, cred)
					if w.err != nil {
						assert.ErrorIs(t, err, w.err)
						assert.Nil(t, res.Principal)
					} else {
						require.NoError(t, err)
						require.NotNil(t, res.Principal)
						assert.Equal(t, w.principal, res.Principal.ID)
						assert.Equal(t, w.method, res.Method)
						assert.Equal(t, w.method, res.Principal.Provider)
						assert.Equal(t, w.ambient, res.Ambient)
						assert.Equal(t, w.ambient, res.Principal.IsSession())
					}
					assert.Equal(t, w.clear, res.ClearCookie, "clear cookie")
				})
			}
		}
	}
}

// Carried methods run before ambient ones whatever the configured order,
// within each group in configured order; a name the build lacks is an
// error.
func TestChain_OrderAndBuild(t *testing.T) {
	f := newFixture(t)
	for _, order := range [][]string{
		{auth.MethodAPIKey, auth.MethodSession},
		{auth.MethodSession, auth.MethodAPIKey},
	} {
		chain, err := auth.ChainFor(order, f.method, f.sessions.Method())
		require.NoError(t, err)
		assert.Equal(t, []string{auth.MethodAPIKey, auth.MethodSession}, chain.Methods(), "configured %v", order)
		assert.Equal(t, "api_key, session", chain.String())
	}
	chain := auth.NewChain(f.sessions.Method(), f.method)
	assert.Equal(t, []string{auth.MethodAPIKey, auth.MethodSession}, chain.Methods())
	assert.True(t, chain.Has(auth.MethodSession))
	var none *auth.Chain
	assert.False(t, none.Has(auth.MethodAPIKey))

	_, err := auth.ChainFor([]string{auth.MethodAPIKey, "proxy"}, f.method)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"proxy"`)
}

// A refusal stops the chain: nothing behind a bad bearer key is tried,
// so a valid session cookie cannot rescue it, and an internal failure
// of a method surfaces as itself.
func TestChain_StopsOnRefusal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	live := f.signIn(f.personKey)
	chain := auth.NewChain(f.method, f.sessions.Method())
	res, err := chain.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: "ctxt_bad", Cookie: live.Secret})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	assert.False(t, res.ClearCookie, "a cookie the chain never examined is left alone")

	boom := errors.New("boom")
	chain = auth.NewChain(failing{err: boom}, f.sessions.Method())
	_, err = chain.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: "x", Cookie: live.Secret})
	assert.ErrorIs(t, err, boom)
}

// Revocation end to end at the chain: revoking the principal fails its
// next key use and its session; revoking one key ends only the sessions
// that key minted.
func TestChain_RevocationEndsKeyAndSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	chain := auth.NewChain(f.method, f.sessions.Method())
	second := f.mint(f.person.ID, "second", nil)
	s1, s2 := f.signIn(f.personKey), f.signIn(second)

	require.NoError(t, f.keys.Revoke(ctx, f.principalOf(second).Meta[auth.MetaKeyID]))
	_, err := chain.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: second})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	res, err := chain.Authenticate(ctx, auth.Credential{Cookie: s2.Secret})
	assert.ErrorIs(t, err, auth.ErrNoCredential)
	assert.True(t, res.ClearCookie)
	res, err = chain.Authenticate(ctx, auth.Credential{Cookie: s1.Secret})
	require.NoError(t, err)
	assert.Equal(t, f.person.ID, res.Principal.ID, "the other key's session lives on")

	require.NoError(t, f.drv.Auth().Principals().Revoke(ctx, f.person.ID, f.clock.t))
	_, err = chain.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: f.personKey})
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	res, err = chain.Authenticate(ctx, auth.Credential{Cookie: s1.Secret})
	assert.ErrorIs(t, err, auth.ErrNoCredential)
	assert.True(t, res.ClearCookie)
	res, err = chain.Authenticate(ctx, auth.Credential{Scheme: auth.SchemeBearer, Token: f.ownerKey})
	require.NoError(t, err)
	assert.Equal(t, storage.OwnerPrincipal, res.Principal.ID, "the owner is untouched")
}

// failing is a carried method that always fails internally.
type failing struct{ err error }

func (failing) Name() string  { return "failing" }
func (failing) Ambient() bool { return false }
func (m failing) Authenticate(context.Context, auth.Credential) (*auth.Principal, bool, error) {
	return nil, false, m.err
}
