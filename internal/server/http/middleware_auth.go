package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// HeaderAPIKey is the API-key header accepted alongside Authorization:
// Bearer. Both carry the same opaque credential; which principal it
// resolves to is entirely up to the api_key method.
const HeaderAPIKey = "X-API-Key"

// RequireAuth returns middleware that authenticates every request
// through the method chain and rejects unauthenticated calls with 401.
// It only lifts credentials out of the request (credentialFromRequest)
// and forwards them; which methods run is the chain's. On success the
// principal and its viewer are attached to the request context
// (attachCaller) for handlers, the policy engine, metering and scoped
// storage reads.
//
// A session cookie the chain found stale is cleared. A request only an
// ambient method authenticated (a session cookie) gets the browser
// rules (ADR-079 §3), since such a credential rides along with any
// request a hostile page makes the browser send: a write must come from
// this instance's own pages (Sec-Fetch-Site or Origin, see
// sameOriginWrite) and carry X-Ctxt-CSRF: 1; a read sent by another
// site or another port on this host is refused too. A carried
// credential (a bearer key) ran first and lifts both.
//
// Failures are recorded on the security emitter (nil-safe), keyed by
// remote host since no principal exists yet.
func RequireAuth(chain *authn.Chain, sec *security.Emitter, devCORS bool) func(http.Handler) http.Handler {
	cop := newSessionCOP(devCORS)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			res, err := chain.Authenticate(r.Context(), credentialFromRequest(r))
			if res.ClearCookie {
				http.SetCookie(w, sessionCookie(r, "", time.Time{}))
			}
			if err != nil {
				if sec != nil {
					sec.RecordAuthFailure(r.Context(), remoteHost(r))
				}
				w.Header().Set("WWW-Authenticate", `Bearer realm="dpkms"`)
				msg := "invalid credentials"
				if errors.Is(err, authn.ErrNoCredential) {
					msg = "authentication required"
				}
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", msg)
				return
			}
			if res.Ambient && !browserRulesPass(w, cop, r) {
				return
			}
			next.ServeHTTP(w, r.WithContext(attachCaller(r.Context(), res.Principal)))
		})
	}
}

// browserRulesPass applies the ambient-identity rules to r and writes
// the refusal when they fail: cross-site reads are refused; writes need
// a same-origin signal and the CSRF header.
func browserRulesPass(w http.ResponseWriter, cop *http.CrossOriginProtection, r *http.Request) bool {
	if isSafeMethod(r.Method) {
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
				"cross-origin request refused: the web UI session answers this instance's own pages only")
			return false
		}
		return true
	}
	if !sameOriginWrite(cop, r) {
		WriteError(w, http.StatusForbidden, "CROSS_ORIGIN_REQUEST",
			"cross-origin request refused: changes must come from this instance's own pages")
		return false
	}
	if r.Header.Get(HeaderCSRF) != "1" {
		WriteError(w, http.StatusForbidden, "CSRF_HEADER_REQUIRED",
			"web UI session writes must send "+HeaderCSRF+": 1")
		return false
	}
	return true
}

// attachCaller binds an authenticated principal and its viewer to ctx
// in one step, so no request reaches a handler authenticated but
// unscoped. Every authenticated principal is Unrestricted until share
// policies resolve a narrower viewer per principal (ADR-080 §4).
func attachCaller(ctx context.Context, princ *authn.Principal) context.Context {
	return sharing.WithViewer(authn.Attach(ctx, princ), sharing.Unrestricted)
}

// remoteHost extracts the peer host from RemoteAddr for security-event
// keying (per-source sliding windows) — no principal exists on failure.
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// credentialFromRequest normalizes transport-level credential material
// (Authorization: Bearer, X-API-Key, the instance's web UI session
// cookie, TLS state) into an authn.Credential so methods never parse
// headers themselves. A header credential and the cookie are both
// carried over: the chain runs carried methods first, so a request
// with both is the header's principal, and a bad header is refused
// whatever cookie stands behind it.
func credentialFromRequest(r *http.Request) authn.Credential {
	cred := authn.Credential{TLS: r.TLS}
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := cutPrefixFold(h, "Bearer "); ok {
			cred.Scheme = authn.SchemeBearer
			cred.Token = strings.TrimSpace(tok)
		}
	}
	if cred.Token == "" {
		if k := r.Header.Get(HeaderAPIKey); k != "" {
			cred.Scheme = authn.SchemeAPIKey
			cred.Token = k
		}
	}
	if c, err := r.Cookie(SessionCookieName(r.Host)); err == nil && c.Value != "" {
		cred.Cookie = c.Value
	}
	return cred
}

// cutPrefixFold is strings.CutPrefix with ASCII case-insensitive
// matching, per RFC 9110 auth-scheme comparison rules.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}
