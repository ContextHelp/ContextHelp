package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// This file is the pre-K4 local form of hop.top/kit proposal K4
// (ADR-079 §6, authn/ticket): single-use, expiring, hashed-at-rest
// tickets with a kind and an opaque payload, redeemed in one atomic
// step. The store (storage.TicketStore) already has K4's Issue/Redeem
// shape; Tickets adds what an invite or a sign-in link means for the
// principal registry and the browser session it ends in.

// Ticket lifetimes (ADR-079 §2).
const (
	// DefaultInviteTTL is how long an invite link can be redeemed when
	// the issuer gives no expiry.
	DefaultInviteTTL = 7 * 24 * time.Hour
	// MagicLinkTTL is how long a sign-in link can be redeemed.
	MagicLinkTTL = 10 * time.Minute
)

// Identity names of the link identities a redemption creates.
const (
	// LinkIdentityInvite names the identity of a redeemed invite.
	LinkIdentityInvite = "invite"
	// LinkIdentitySignIn names the identity of a redeemed sign-in link.
	LinkIdentitySignIn = "sign-in link"
)

// ErrInvalidTicket is a redemption of a ticket that is unknown, of
// another kind, already redeemed or expired. It wraps
// ErrInvalidCredential.
var ErrInvalidTicket = fmt.Errorf("%w: ticket is unknown, used or expired", ErrInvalidCredential)

// InvitePayload is what an invite ticket carries (the grants column):
// the display name of the person it creates. Audiences and roles
// arrive with sharing (ADR-080).
type InvitePayload struct {
	DisplayName string `json:"display_name"`
}

// Issued is a minted ticket: the secret, shown once, and its expiry.
type Issued struct {
	Secret    string
	ExpiresAt time.Time
}

// Tickets mints and redeems invite and sign-in links over the principal
// registry. A redemption creates a link identity on the principal
// (method storage.IdentityMethodLink, subject the ticket's hash) and
// mints a browser session bound to it, so revoking that identity ends
// the session. Redeem is atomic in the store: a second redemption of
// one secret finds no ticket.
type Tickets struct {
	auth     storage.AuthStore
	sessions *Sessions
	now      func() time.Time
}

// NewTickets returns Tickets over the registry in auth, minting
// sessions through sessions. now is the clock; nil means time.Now.
func NewTickets(auth storage.AuthStore, sessions *Sessions, now func() time.Time) (*Tickets, error) {
	if auth == nil {
		return nil, errors.New("auth: tickets need the principal registry")
	}
	if sessions == nil {
		return nil, errors.New("auth: tickets need the session manager")
	}
	if now == nil {
		now = time.Now
	}
	return &Tickets{auth: auth, sessions: sessions, now: now}, nil
}

// MintInvite issues an invite for a person to be called displayName,
// redeemable for ttl (DefaultInviteTTL when zero), on behalf of by. An
// empty display name or a negative ttl is an error.
func (t *Tickets) MintInvite(ctx context.Context, by *Principal, displayName string, ttl time.Duration) (Issued, error) {
	if by == nil {
		return Issued{}, ErrNoCredential
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return Issued{}, errors.New("auth: an invite needs the display name of the person invited")
	}
	if ttl < 0 {
		return Issued{}, errors.New("auth: invite expiry must be positive")
	}
	if ttl == 0 {
		ttl = DefaultInviteTTL
	}
	payload, err := json.Marshal(InvitePayload{DisplayName: displayName})
	if err != nil {
		return Issued{}, fmt.Errorf("auth: encode invite: %w", err)
	}
	now := t.now().UTC()
	secret, err := t.auth.Tickets().Issue(ctx, storage.TicketIssue{
		Kind: storage.TicketKindInvite, Payload: payload, TTL: ttl, CreatedBy: by.ID, Now: now,
	})
	if err != nil {
		return Issued{}, err
	}
	return Issued{Secret: secret, ExpiresAt: now.Add(ttl)}, nil
}

// MintLink issues a sign-in link for principalID, redeemable for
// MagicLinkTTL, on behalf of by. An unknown principal is
// storage.ErrNotFound; a suspended or revoked one is
// storage.ErrPrincipalInactive.
func (t *Tickets) MintLink(ctx context.Context, by *Principal, principalID string) (Issued, error) {
	if by == nil {
		return Issued{}, ErrNoCredential
	}
	p, err := t.auth.Principals().Get(ctx, principalID)
	if err != nil {
		return Issued{}, err
	}
	if !p.Active() {
		return Issued{}, fmt.Errorf("auth: principal %s is %s: %w", p.ID, p.Status, storage.ErrPrincipalInactive)
	}
	now := t.now().UTC()
	secret, err := t.auth.Tickets().Issue(ctx, storage.TicketIssue{
		Kind: storage.TicketKindMagicLink, TTL: MagicLinkTTL, CreatedBy: by.ID, PrincipalID: p.ID, Now: now,
	})
	if err != nil {
		return Issued{}, err
	}
	return Issued{Secret: secret, ExpiresAt: now.Add(MagicLinkTTL)}, nil
}

// RedeemInvite redeems an invite: it creates a person principal with
// the invite's display name and the default roles, binds a link
// identity to it and mints a browser session. A secret that is unknown,
// used or expired is ErrInvalidTicket; the ticket is spent either way.
func (t *Tickets) RedeemInvite(ctx context.Context, secret string, ci ClientInfo) (*Minted, error) {
	tk, err := t.redeem(ctx, storage.TicketKindInvite, secret)
	if err != nil {
		return nil, err
	}
	var payload InvitePayload
	if err := json.Unmarshal(tk.Payload, &payload); err != nil {
		return nil, fmt.Errorf("auth: decode invite: %w", err)
	}
	now := t.now().UTC()
	p := &storage.Principal{
		Kind: storage.PrincipalKindPerson, DisplayName: payload.DisplayName, CreatedAt: now,
	}
	if err := t.auth.Principals().Create(ctx, p); err != nil {
		return nil, fmt.Errorf("auth: create invited principal: %w", err)
	}
	return t.bindAndMint(ctx, p.ID, storage.TicketHash(secret), LinkIdentityInvite, now, ci)
}

// RedeemLink redeems a sign-in link: it binds a fresh link identity to
// the ticket's principal and mints a browser session. A secret that is
// unknown, used or expired, or whose principal is suspended or
// revoked, is ErrInvalidTicket.
func (t *Tickets) RedeemLink(ctx context.Context, secret string, ci ClientInfo) (*Minted, error) {
	tk, err := t.redeem(ctx, storage.TicketKindMagicLink, secret)
	if err != nil {
		return nil, err
	}
	return t.bindAndMint(ctx, tk.PrincipalID, storage.TicketHash(secret), LinkIdentitySignIn, t.now().UTC(), ci)
}

// redeem spends the ticket for secret, mapping the store's refusals to
// ErrInvalidTicket. A ticket of an inactive principal is refused the
// same way: the caller learns nothing about the principal.
func (t *Tickets) redeem(ctx context.Context, kind, secret string) (*storage.Ticket, error) {
	if secret == "" {
		return nil, ErrNoCredential
	}
	tk, err := t.auth.Tickets().Redeem(ctx, kind, secret, t.now().UTC())
	switch {
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, storage.ErrPrincipalInactive):
		return nil, ErrInvalidTicket
	case err != nil:
		return nil, err
	}
	return tk, nil
}

// bindAndMint creates the link identity (subject = the ticket hash) on
// principalID and mints the session bound to it.
func (t *Tickets) bindAndMint(ctx context.Context, principalID, subject, name string, now time.Time, ci ClientInfo) (*Minted, error) {
	idn := &storage.Identity{
		PrincipalID: principalID, Method: storage.IdentityMethodLink, Subject: subject, Name: name, CreatedAt: now,
	}
	if err := t.auth.Identities().Create(ctx, idn); err != nil {
		return nil, fmt.Errorf("auth: bind link identity: %w", err)
	}
	return t.sessions.MintFor(ctx, principalID, idn.ID, ci)
}
