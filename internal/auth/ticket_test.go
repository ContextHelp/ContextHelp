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
)

var browser = auth.ClientInfo{UserAgent: "test", RemoteAddr: "127.0.0.1"}

func (f *fixture) tickets() *auth.Tickets {
	f.t.Helper()
	tk, err := auth.NewTickets(f.drv.Auth(), f.sessions, f.clock.now)
	require.NoError(f.t, err)
	return tk
}

func TestTickets_InviteCreatesPersonWithLinkIdentityAndSession(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	owner := f.principalOf(f.ownerKey)

	issued, err := tk.MintInvite(ctx, owner, "  Bob Partner ", 0)
	require.NoError(t, err)
	assert.Len(t, issued.Secret, 43, "256-bit base64url secret")
	assert.Equal(t, f.clock.t.Add(auth.DefaultInviteTTL), issued.ExpiresAt)

	m, err := tk.RedeemInvite(ctx, issued.Secret, browser)
	require.NoError(t, err)
	p := m.Principal
	assert.True(t, strings.HasPrefix(p.ID, "prn_"), "fresh principal id, got %s", p.ID)
	assert.Equal(t, storage.PrincipalKindPerson, p.Kind)
	assert.Equal(t, "Bob Partner", p.Name, "display name trimmed")
	assert.Equal(t, []string{auth.RoleReader}, p.Roles)
	assert.True(t, p.IsSession())
	assert.Equal(t, auth.SessionScopesFor(auth.ScopesForRoles([]string{auth.RoleReader}), auth.SessionKindUI), p.Scopes)

	idn, err := f.drv.Auth().Identities().Get(ctx, m.Session.IdentityID)
	require.NoError(t, err)
	assert.Equal(t, storage.IdentityMethodLink, idn.Method)
	assert.Equal(t, storage.TicketHash(issued.Secret), idn.Subject, "subject is the ticket hash")
	assert.Equal(t, auth.LinkIdentityInvite, idn.Name)
	assert.Equal(t, p.ID, idn.PrincipalID)

	// The cookie authenticates as the person.
	again, _, err := f.sessions.Authenticate(ctx, m.Secret)
	require.NoError(t, err)
	assert.Equal(t, p.ID, again.ID)
}

func TestTickets_InviteIsSingleUse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	issued, err := tk.MintInvite(ctx, f.principalOf(f.ownerKey), "Bob", time.Hour)
	require.NoError(t, err)

	_, err = tk.RedeemInvite(ctx, issued.Secret, browser)
	require.NoError(t, err)
	_, err = tk.RedeemInvite(ctx, issued.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)

	people, err := f.drv.Auth().Principals().List(ctx, storage.PrincipalFilter{Kind: storage.PrincipalKindPerson})
	require.NoError(t, err)
	assert.Len(t, people, 2, "the fixture's person plus one invitee, never two")
}

func TestTickets_InviteExpires(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	issued, err := tk.MintInvite(ctx, f.principalOf(f.ownerKey), "Bob", time.Hour)
	require.NoError(t, err)
	f.clock.advance(time.Hour)
	_, err = tk.RedeemInvite(ctx, issued.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)
}

func TestTickets_InviteRefusals(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	owner := f.principalOf(f.ownerKey)

	_, err := tk.MintInvite(ctx, owner, "   ", time.Hour)
	assert.ErrorContains(t, err, "display name")
	_, err = tk.MintInvite(ctx, owner, "Bob", -time.Hour)
	assert.ErrorContains(t, err, "positive")
	_, err = tk.MintInvite(ctx, nil, "Bob", time.Hour)
	assert.ErrorIs(t, err, auth.ErrNoCredential)

	_, err = tk.RedeemInvite(ctx, "", browser)
	assert.ErrorIs(t, err, auth.ErrNoCredential)
	_, err = tk.RedeemInvite(ctx, strings.Repeat("A", 43), browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)

	// A sign-in link is not an invite.
	link, err := tk.MintLink(ctx, owner, f.person.ID)
	require.NoError(t, err)
	_, err = tk.RedeemInvite(ctx, link.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)
}

func TestTickets_LinkSignsPrincipalInThroughFreshIdentity(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()

	issued, err := tk.MintLink(ctx, f.principalOf(f.ownerKey), f.person.ID)
	require.NoError(t, err)
	assert.Equal(t, f.clock.t.Add(auth.MagicLinkTTL), issued.ExpiresAt)

	m, err := tk.RedeemLink(ctx, issued.Secret, browser)
	require.NoError(t, err)
	assert.Equal(t, f.person.ID, m.Principal.ID)
	assert.Equal(t, []string{auth.RoleReader}, m.Principal.Roles)
	idn, err := f.drv.Auth().Identities().Get(ctx, m.Session.IdentityID)
	require.NoError(t, err)
	assert.Equal(t, storage.IdentityMethodLink, idn.Method)
	assert.Equal(t, storage.TicketHash(issued.Secret), idn.Subject)
	assert.Equal(t, auth.LinkIdentitySignIn, idn.Name)

	_, err = tk.RedeemLink(ctx, issued.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket, "single use")

	// Revoking the link identity ends the session it minted and nothing
	// else: the person's key still works.
	require.NoError(t, f.drv.Auth().Identities().Revoke(ctx, idn.ID, f.clock.now()))
	_, _, err = f.sessions.Authenticate(ctx, m.Secret)
	assert.ErrorIs(t, err, auth.ErrInvalidCredential)
	f.principalOf(f.personKey)
}

func TestTickets_LinkRefusedForInactivePrincipal(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	owner := f.principalOf(f.ownerKey)

	_, err := tk.MintLink(ctx, owner, "prn_nobody")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// Minted while active, redeemed after suspension: refused, and the
	// refusal says nothing about why.
	issued, err := tk.MintLink(ctx, owner, f.person.ID)
	require.NoError(t, err)
	require.NoError(t, f.drv.Auth().Principals().Suspend(ctx, f.person.ID))
	_, err = tk.RedeemLink(ctx, issued.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)

	// Minting for a suspended principal is refused outright.
	_, err = tk.MintLink(ctx, owner, f.person.ID)
	assert.ErrorIs(t, err, storage.ErrPrincipalInactive)

	require.NoError(t, f.drv.Auth().Principals().Revoke(ctx, f.person.ID, f.clock.now()))
	_, err = tk.MintLink(ctx, owner, f.person.ID)
	assert.ErrorIs(t, err, storage.ErrPrincipalInactive)
}

func TestTickets_LinkExpires(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	tk := f.tickets()
	issued, err := tk.MintLink(ctx, f.principalOf(f.ownerKey), f.person.ID)
	require.NoError(t, err)
	f.clock.advance(auth.MagicLinkTTL)
	_, err = tk.RedeemLink(ctx, issued.Secret, browser)
	assert.ErrorIs(t, err, auth.ErrInvalidTicket)
}

func TestNewTickets_NeedsRegistryAndSessions(t *testing.T) {
	f := newFixture(t)
	_, err := auth.NewTickets(nil, f.sessions, nil)
	assert.Error(t, err)
	_, err = auth.NewTickets(f.drv.Auth(), nil, nil)
	assert.Error(t, err)
}
