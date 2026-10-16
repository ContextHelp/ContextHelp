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

	issued, err := tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "  Bob Partner "})
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
	issued, err := tk.MintInvite(ctx, f.principalOf(f.ownerKey), auth.Invite{DisplayName: "Bob", TTL: time.Hour})
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
	issued, err := tk.MintInvite(ctx, f.principalOf(f.ownerKey), auth.Invite{DisplayName: "Bob", TTL: time.Hour})
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

	_, err := tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "   ", TTL: time.Hour})
	assert.ErrorContains(t, err, "display name")
	_, err = tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "Bob", TTL: -time.Hour})
	assert.ErrorContains(t, err, "positive")
	_, err = tk.MintInvite(ctx, nil, auth.Invite{DisplayName: "Bob", TTL: time.Hour})
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

// An invite minted into an audience lands the redeemed person in it,
// added by the minter with no expiry (ADR-080 §11). Naming an audience
// needs the sharing registry and an audience that exists.
func TestTickets_InviteIntoAudience(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	owner := f.principalOf(f.ownerKey)
	aud := &storage.Audience{Name: "partners"}
	require.NoError(t, f.drv.Sharing().Audiences().Create(ctx, aud))

	bare := f.tickets()
	_, err := bare.MintInvite(ctx, owner, auth.Invite{DisplayName: "Bob", AudienceID: aud.ID})
	assert.ErrorContains(t, err, "no sharing registry")

	tk, err := auth.NewTickets(f.drv.Auth(), f.sessions, f.clock.now, auth.WithAudiences(f.drv.Sharing()))
	require.NoError(t, err)
	_, err = tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "Bob", AudienceID: "aud_missing"})
	assert.ErrorIs(t, err, storage.ErrNotFound)

	issued, err := tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "Bob", AudienceID: aud.ID})
	require.NoError(t, err)
	m, err := tk.RedeemInvite(ctx, issued.Secret, browser)
	require.NoError(t, err)

	members, err := f.drv.Sharing().Audiences().ListMembers(ctx, aud.ID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	assert.Equal(t, m.Principal.ID, members[0].PrincipalID)
	assert.Equal(t, owner.ID, members[0].AddedBy, "added by the invite's minter")
	assert.Nil(t, members[0].ExpiresAt)

	// Without an audience nothing joins.
	issued, err = tk.MintInvite(ctx, owner, auth.Invite{DisplayName: "Sam"})
	require.NoError(t, err)
	m, err = tk.RedeemInvite(ctx, issued.Secret, browser)
	require.NoError(t, err)
	got, err := f.drv.Sharing().Audiences().ListMemberships(ctx, m.Principal.ID, f.clock.now())
	require.NoError(t, err)
	assert.Empty(t, got)
}
