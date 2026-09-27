package http

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Web UI sign-in, in three steps:
//
//  1. The ctxt CLI, authenticated with its static token, calls
//     POST /api/v1/ui/login-codes and gets a single-use code.
//  2. It opens /ui/auth#code=<code>. The fragment never reaches the
//     server, proxies or Referer.
//  3. The page POSTs the code to /ui/auth/session and receives the
//     session cookie: HttpOnly, Secure, SameSite=Strict, named per
//     instance (SessionCookieName).
//
// GET /api/v1/ui/session says who the caller is; DELETE signs out.

// HeaderCSRF must accompany every cookie-authenticated write and the
// code exchange. A cross-origin page cannot set it without a CORS
// preflight, which this server never approves for another origin.
const HeaderCSRF = "X-Ctxt-CSRF"

// LoginPath is the web UI route that exchanges a login code.
const LoginPath = "/ui/auth"

// sessionCookiePrefix makes browsers refuse the cookie unless it is
// Secure, host-only and Path=/, so no sibling host can plant or shadow it.
const sessionCookiePrefix = "__Host-dpkms_"

// maxExchangeBody bounds the code exchange request body.
const maxExchangeBody = 4 << 10

// SessionCookieName returns the session cookie name for requests
// carrying host (the Host header, port included). Browsers share one
// cookie jar across the ports of a host, so two dpkms instances on one
// machine would overwrite each other's cookie under a fixed name.
func SessionCookieName(host string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host)))
	return sessionCookiePrefix + hex.EncodeToString(sum[:8])
}

func sessionCookie(r *http.Request, value string, expires time.Time) *http.Cookie {
	c := &http.Cookie{
		Name:     SessionCookieName(r.Host),
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	if value == "" {
		c.MaxAge = -1
		return c
	}
	c.Expires = expires.UTC()
	c.MaxAge = max(int(time.Until(expires).Seconds()), 1)
	return c
}

// sameOriginWrite reports whether a state-changing request comes from
// this instance's own pages. It reuses net/http's CrossOriginProtection
// (Sec-Fetch-Site, falling back to Origin versus Host) and, unlike the
// router-wide guard, also refuses a request with neither header: every
// browser sends one on a write, so its absence means the request did
// not come from the web UI and has no business carrying the cookie.
func sameOriginWrite(cop *http.CrossOriginProtection, r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "" && r.Header.Get("Origin") == "" {
		return false
	}
	return cop.Check(r) == nil
}

func newSessionCOP(devCORS bool) *http.CrossOriginProtection {
	cop := http.NewCrossOriginProtection()
	if devCORS {
		for _, o := range allowedDevOrigins {
			if err := cop.AddTrustedOrigin(o); err != nil {
				panic("csrf: invalid dev origin " + o + ": " + err.Error())
			}
		}
	}
	return cop
}

// uiSessionRoutes serves the sign-in endpoints. sessions is nil on a
// private instance, which needs no sign-in.
type uiSessionRoutes struct {
	sessions *authn.Sessions
	sec      *security.Emitter
	cop      *http.CrossOriginProtection
	// warn receives operator warnings (plain-HTTP sign-in).
	warn io.Writer
}

func newUISessionRoutes(rc RouterConfig) *uiSessionRoutes {
	return &uiSessionRoutes{sessions: rc.Sessions, sec: rc.Security, cop: newSessionCOP(rc.DevCORS), warn: os.Stderr}
}

type loginCodeResponse struct {
	// SessionRequired is false on a private instance: open /ui/ as is.
	SessionRequired bool      `json:"session_required"`
	Code            string    `json:"code,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitzero"`
	ExpiresIn       int       `json:"expires_in,omitempty"`
	// LoginPath is where to send the code, in the URL fragment.
	LoginPath string `json:"login_path"`
}

// mintCode handles POST /api/v1/ui/login-codes. Only a token caller may
// mint: a browser session minting codes could extend itself forever.
func (u *uiSessionRoutes) mintCode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if u.sessions == nil {
		WriteJSON(w, http.StatusOK, loginCodeResponse{SessionRequired: false, LoginPath: "/ui/"})
		return
	}
	if p, _ := authn.FromContext(r.Context()); p.IsSession() {
		WriteError(w, http.StatusForbidden, "SESSION_SCOPE", "a browser session cannot mint login codes")
		return
	}
	cred := credentialFromRequest(r)
	if cred.Scheme != authn.SchemeBearer && cred.Scheme != authn.SchemeAPIKey {
		WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "login codes need an API token")
		return
	}
	code, expires, err := u.sessions.MintCode(r.Context(), cred.Token)
	if err != nil {
		if errors.Is(err, authn.ErrInvalidCredential) || errors.Is(err, authn.ErrNoCredential) {
			WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "login codes need a configured API token")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not mint a login code")
		return
	}
	WriteJSON(w, http.StatusCreated, loginCodeResponse{
		SessionRequired: true,
		Code:            code,
		ExpiresAt:       expires,
		ExpiresIn:       int(u.sessions.CodeTTL().Seconds()),
		LoginPath:       LoginPath,
	})
}

type sessionInfo struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	IdleExpiresAt time.Time `json:"idle_expires_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}

type uiSessionResponse struct {
	Authenticated   bool         `json:"authenticated"`
	SessionRequired bool         `json:"session_required"`
	Principal       string       `json:"principal,omitempty"`
	Via             string       `json:"via,omitempty"`
	Scope           string       `json:"scope,omitempty"`
	Session         *sessionInfo `json:"session,omitempty"`
	// Warning names a condition that will break the session, such as
	// plain HTTP to a remote host.
	Warning string `json:"warning,omitempty"`
}

func toSessionInfo(s *storage.UISession) *sessionInfo {
	return &sessionInfo{ID: s.ID, CreatedAt: s.CreatedAt, IdleExpiresAt: s.IdleExpiresAt, ExpiresAt: s.ExpiresAt}
}

// whoami handles GET /api/v1/ui/session.
func (u *uiSessionRoutes) whoami(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := authn.FromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusOK, uiSessionResponse{SessionRequired: u.sessions != nil})
		return
	}
	resp := uiSessionResponse{Authenticated: true, SessionRequired: u.sessions != nil, Principal: p.ID, Via: "token"}
	if p.IsSession() {
		resp.Via = authn.ViaSession
		resp.Scope = p.SessionScope()
		if u.sessions != nil {
			if s, err := u.sessions.Get(r.Context(), p.Meta[authn.MetaSessionID]); err == nil {
				resp.Session = toSessionInfo(s)
			}
		}
	}
	WriteJSON(w, http.StatusOK, resp)
}

// signOut handles DELETE /api/v1/ui/session: it revokes the caller's
// session and clears the cookie.
func (u *uiSessionRoutes) signOut(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, _ := authn.FromContext(r.Context())
	if u.sessions == nil || !p.IsSession() {
		WriteError(w, http.StatusBadRequest, "NO_SESSION", "this request is not signed in with a browser session")
		return
	}
	if err := u.sessions.Revoke(r.Context(), p.Meta[authn.MetaSessionID], authn.RevokeReasonLogout); err != nil &&
		!errors.Is(err, storage.ErrNotFound) {
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not end the session")
		return
	}
	http.SetCookie(w, sessionCookie(r, "", time.Time{}))
	w.WriteHeader(http.StatusNoContent)
}

// exchange handles POST /ui/auth/session: a login code for a cookie.
// It sits outside /api/v1 because the caller has no credential yet.
func (u *uiSessionRoutes) exchange(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if u.sessions == nil {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "this instance needs no sign-in; open /ui/")
		return
	}
	if !sameOriginWrite(u.cop, r) {
		WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST", "sign-in must come from this instance's own web UI")
		return
	}
	if r.Header.Get(HeaderCSRF) != "1" {
		WriteError(w, http.StatusForbidden, "CSRF_HEADER_REQUIRED", "sign-in must send "+HeaderCSRF+": 1")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxExchangeBody)).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
		return
	}
	m, err := u.sessions.Exchange(r.Context(), req.Code, authn.ClientInfo{
		UserAgent:  r.UserAgent(),
		RemoteAddr: remoteHost(r),
	})
	if err != nil {
		if errors.Is(err, authn.ErrInvalidCredential) || errors.Is(err, authn.ErrNoCredential) {
			if u.sec != nil {
				u.sec.RecordAuthFailure(r.Context(), remoteHost(r))
			}
			WriteError(w, http.StatusUnauthorized, "INVALID_LOGIN_CODE",
				"this sign-in link is invalid, used or expired: run `ctxt ui open` again")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "could not start a session")
		return
	}
	http.SetCookie(w, sessionCookie(r, m.Secret, m.Session.ExpiresAt))
	resp := uiSessionResponse{
		Authenticated:   true,
		SessionRequired: true,
		Principal:       m.Principal.ID,
		Via:             authn.ViaSession,
		Scope:           m.Session.Scope,
		Session:         toSessionInfo(m.Session),
	}
	if plainHTTPRemote(r) {
		resp.Warning = plainHTTPWarning
		fmt.Fprintf(u.warn, "warning: web UI sign-in over plain HTTP from %s to %s: browsers drop the Secure session cookie there; serve dpkms behind TLS\n",
			remoteHost(r), r.Host)
	}
	WriteJSON(w, http.StatusOK, resp)
}

const plainHTTPWarning = "this instance is reached over plain HTTP: browsers keep the Secure session cookie only over HTTPS or on localhost, so the sign-in will not stick"

// plainHTTPRemote reports whether r reached dpkms over plain HTTP under
// a non-loopback name, where browsers refuse Secure cookies. The
// X-Forwarded-Proto header is read only to stay quiet behind a TLS
// proxy; it grants nothing.
func plainHTTPRemote(r *http.Request) bool {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	ip := net.ParseIP(host)
	return ip == nil || !ip.IsLoopback()
}
