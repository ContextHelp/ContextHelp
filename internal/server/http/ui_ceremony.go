package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Invite and sign-in links (ADR-079 §2), the two ticket ceremonies:
//
//   - POST /api/v1/share/invites (admin:shares) mints an invite and
//     answers the link https://<public_url>/ui/invite#t=<secret>. The
//     page POSTs the secret to /ui/invite/session, which creates a
//     person principal, binds a link identity to it and sets the
//     session cookie.
//   - POST /api/v1/principals/{id}/links (admin:principals) mints a
//     sign-in link for an existing principal, valid 10 minutes, at
//     https://<public_url>/ui/signin#t=<secret>; /ui/signin/session
//     redeems it the same way.
//
// The secret rides in the URL fragment, as login codes do, so it never
// reaches logs, proxies or Referer. Every redemption is single use,
// rate limited per client address (LimitByAddr) and refused on a
// request that already carries a live session: the caller signs out
// first, so a ticket can never bind to another principal's browser.

// Web UI routes the links point at; each redeems at <path>/session.
const (
	InvitePath = "/ui/invite"
	SignInPath = "/ui/signin"
)

// Error codes of the ceremonies.
const (
	// CodeSignInDisabled is a ceremony server.auth.sign_in does not list,
	// or a mint on an instance with no browser sign-in.
	CodeSignInDisabled = "SIGN_IN_DISABLED"
	// CodeSessionActive is a redemption or exchange on a request that
	// already carries a live session.
	CodeSessionActive = "SESSION_ACTIVE"
	// CodeInvalidTicket is a redemption of a ticket that is unknown,
	// used or expired.
	CodeInvalidTicket = "INVALID_TICKET"
)

// linkResponse is the body of a minted invite or sign-in link.
type linkResponse struct {
	// URL is the link to hand to the person; the secret is in its
	// fragment.
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	// ExpiresIn is the link's lifetime in seconds at minting.
	ExpiresIn int `json:"expires_in"`
}

// ceremonyOn reports whether the named ceremony may mint a session
// here, writing the refusal when it may not.
func (u *uiSessionRoutes) ceremonyOn(w http.ResponseWriter, name string) bool {
	if u.sessions == nil || u.tickets == nil {
		WriteError(w, http.StatusNotFound, CodeSignInDisabled,
			"this instance has no browser sign-in: links need a protected or public instance running the session method")
		return false
	}
	if !slices.Contains(u.signIn, name) {
		WriteError(w, http.StatusNotFound, CodeSignInDisabled,
			fmt.Sprintf("%s links are not enabled on this instance: add %q to server.auth.sign_in", name, name))
		return false
	}
	return true
}

// link builds the URL for secret at path under the public URL.
func (u *uiSessionRoutes) link(path string, issued authn.Issued) linkResponse {
	return linkResponse{
		URL:       strings.TrimRight(u.publicURL, "/") + path + "#t=" + issued.Secret,
		ExpiresAt: issued.ExpiresAt,
		ExpiresIn: int(time.Until(issued.ExpiresAt).Round(time.Second).Seconds()),
	}
}

// inviteRequest is the body of POST /api/v1/share/invites.
type inviteRequest struct {
	// DisplayName is what the invited person will be called.
	DisplayName string `json:"display_name"`
	// ExpiresIn is the link's lifetime in seconds; 0 means the default
	// (7 days).
	ExpiresIn int `json:"expires_in"`
}

// mintInvite handles POST /api/v1/share/invites, behind admin:shares.
func (u *uiSessionRoutes) mintInvite(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !u.ceremonyOn(w, config.AuthSignInInvite) {
		return
	}
	var req inviteRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxExchangeBody)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if req.ExpiresIn < 0 {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "expires_in must be positive")
		return
	}
	p, _ := authn.FromContext(r.Context())
	issued, err := u.tickets.MintInvite(r.Context(), p, req.DisplayName, time.Duration(req.ExpiresIn)*time.Second)
	switch {
	case errors.Is(err, authn.ErrNoCredential):
		WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not mint an invite")
		return
	case err != nil && strings.HasPrefix(err.Error(), "auth: "):
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", strings.TrimPrefix(err.Error(), "auth: "))
		return
	case err != nil:
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not mint an invite")
		return
	}
	WriteJSON(w, http.StatusCreated, u.link(InvitePath, issued))
}

// mintLink handles POST /api/v1/principals/{id}/links, behind
// admin:principals.
func (u *uiSessionRoutes) mintLink(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !u.ceremonyOn(w, config.AuthSignInLink) {
		return
	}
	id := chi.URLParam(r, "id")
	if !ownerGuard(w, r, id) {
		return
	}
	// A link signs the browser in as the principal: minting one for a
	// principal whose roles grant more than the caller holds would hand
	// the caller that principal.
	target, err := u.reg.Principals().Get(r.Context(), id)
	if err != nil {
		writeRegistryError(w, err, "principal")
		return
	}
	if !withinCeiling(w, r, authn.ScopesForRoles(target.Roles), "a sign-in link for principal "+target.ID) {
		return
	}
	p, _ := authn.FromContext(r.Context())
	issued, err := u.tickets.MintLink(r.Context(), p, id)
	switch {
	case errors.Is(err, authn.ErrNoCredential):
		WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
		return
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "no such principal")
		return
	case errors.Is(err, storage.ErrPrincipalInactive):
		WriteError(w, http.StatusConflict, CodePrincipalInactive, "the principal is suspended or revoked; a sign-in link needs an active one")
		return
	case err != nil:
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not mint a sign-in link")
		return
	}
	WriteJSON(w, http.StatusCreated, u.link(SignInPath, issued))
}

// redeemInvite handles POST /ui/invite/session.
func (u *uiSessionRoutes) redeemInvite(w http.ResponseWriter, r *http.Request) {
	u.redeem(w, r, config.AuthSignInInvite, u.tickets.RedeemInvite)
}

// redeemLink handles POST /ui/signin/session.
func (u *uiSessionRoutes) redeemLink(w http.ResponseWriter, r *http.Request) {
	u.redeem(w, r, config.AuthSignInLink, u.tickets.RedeemLink)
}

// redeem runs one ticket ceremony: the browser checks of the code
// exchange, the active-session refusal, then redeemFn.
func (u *uiSessionRoutes) redeem(w http.ResponseWriter, r *http.Request, ceremony string,
	redeemFn func(ctx context.Context, secret string, ci authn.ClientInfo) (*authn.Minted, error),
) {
	w.Header().Set("Cache-Control", "no-store")
	if u.sessions == nil {
		WriteError(w, http.StatusNotFound, CodeNotFound, "this instance needs no sign-in; open /ui/")
		return
	}
	if !u.ceremonyOn(w, ceremony) {
		return
	}
	if !u.browserChecks(w, r) {
		return
	}
	var req struct {
		Ticket string `json:"ticket"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxExchangeBody)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
		return
	}
	m, err := redeemFn(r.Context(), req.Ticket, authn.ClientInfo{UserAgent: r.UserAgent(), RemoteAddr: remoteHost(r)})
	if err != nil {
		if errors.Is(err, authn.ErrInvalidCredential) || errors.Is(err, authn.ErrNoCredential) {
			if u.sec != nil {
				u.sec.RecordAuthFailure(r.Context(), remoteHost(r))
			}
			WriteError(w, http.StatusUnauthorized, CodeInvalidTicket,
				"this link is invalid, already used or expired: ask for a new one")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not start a session")
		return
	}
	u.finishSignIn(w, r, m)
}
