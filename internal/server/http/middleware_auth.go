package http

import (
	"errors"
	"net/http"
	"strings"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
)

// HeaderAPIKey is the API-key header accepted alongside Authorization:
// Bearer. Both carry the same opaque credential; which identities they
// resolve to is entirely up to the configured auth provider.
const HeaderAPIKey = "X-API-Key"

// RequireAuth returns middleware that authenticates every request
// through the configured provider and rejects unauthenticated calls
// with 401. The concrete scheme lives behind the authn.Provider
// interface — this middleware only extracts transport credentials and
// forwards them. On success the principal is attached to the request
// context for handlers, the policy engine, and metering.
func RequireAuth(provider authn.Provider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			princ, err := provider.Authenticate(r.Context(), credentialFromRequest(r))
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="dpkms"`)
				msg := "invalid credentials"
				if errors.Is(err, authn.ErrNoCredential) {
					msg = "authentication required"
				}
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", msg)
				return
			}
			next.ServeHTTP(w, r.WithContext(authn.WithPrincipal(r.Context(), princ)))
		})
	}
}

// credentialFromRequest normalizes transport-level credential material
// (Authorization: Bearer, X-API-Key, TLS state) into an authn.Credential
// so providers never parse headers themselves.
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
