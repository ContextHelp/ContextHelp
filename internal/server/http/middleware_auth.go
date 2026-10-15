package http

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// HeaderAPIKey is the API-key header accepted alongside Authorization:
// Bearer. Both carry the same opaque credential; which identities they
// resolve to is entirely up to the configured auth provider.
const HeaderAPIKey = "X-API-Key"

// RequireAuth returns middleware that authenticates every request
// through the configured provider and rejects unauthenticated calls
// with 401. The concrete scheme lives behind the authn.Provider
// interface — this middleware only extracts transport credentials and
// forwards them. On success the principal and its viewer are attached
// to the request context (attachCaller) for handlers, the policy
// engine, metering and scoped storage reads. Failures are recorded on
// the security emitter (nil-safe), keyed by remote host since no
// principal exists yet.
func RequireAuth(provider authn.Provider, sec *security.Emitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, err := provider.Authenticate(r.Context(), credentialFromRequest(r))
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
			next.ServeHTTP(w, r.WithContext(attachCaller(r.Context(), princ)))
		})
	}
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
// cookie, TLS state) into an authn.Credential so providers never parse
// headers themselves. A header credential wins over the cookie: a
// browser never sends one, so a request carrying both is a script
// that chose its identity.
func credentialFromRequest(r *http.Request) authn.Credential {
	cred := authn.Credential{TLS: r.TLS}
	if h := r.Header.Get("Authorization"); h != "" {
		if tok, ok := cutPrefixFold(h, "Bearer "); ok {
			cred.Scheme = authn.SchemeBearer
			cred.Token = strings.TrimSpace(tok)
			return cred
		}
	}
	if k := r.Header.Get(HeaderAPIKey); k != "" {
		cred.Scheme = authn.SchemeAPIKey
		cred.Token = k
		return cred
	}
	if c, err := r.Cookie(SessionCookieName(r.Host)); err == nil && c.Value != "" {
		cred.Scheme = authn.SchemeSession
		cred.Token = c.Value
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
