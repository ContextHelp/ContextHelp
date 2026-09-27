package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
)

// CodeInsufficientScope is the error code of a 403 caused by a missing
// scope. It names RFC 6750's insufficient_scope error.
const CodeInsufficientScope = "INSUFFICIENT_SCOPE"

// RequireScope returns middleware that lets a request through only when
// the principal RequireAuth attached holds scope. A missing principal
// fails closed with 401: the middleware must sit behind RequireAuth.
// A principal without the scope gets 403 INSUFFICIENT_SCOPE naming the
// missing scope, plus an RFC 6750 WWW-Authenticate challenge, and the
// denial is recorded on the security emitter (nil-safe).
func RequireScope(scope authn.Scope, sec *security.Emitter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := authn.FromContext(r.Context())
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="dpkms"`)
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
				return
			}
			if !p.HasScope(scope) {
				if sec != nil {
					sec.RecordACLDenial(r.Context(), p.ID)
				}
				writeInsufficientScope(w, scope, p.IsSession())
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeInsufficientScope answers 403 INSUFFICIENT_SCOPE naming scope.
// A browser session is told where the scope lives: its set is the web
// UI's, whatever its token holds.
func writeInsufficientScope(w http.ResponseWriter, scope authn.Scope, session bool) {
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="dpkms", error="insufficient_scope", scope=%q`, string(scope)))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	msg := fmt.Sprintf("missing scope %s", scope)
	if session {
		msg += ": not available to a web UI session; use the ctxt CLI or an API token"
	}
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Code:    CodeInsufficientScope,
		Message: msg,
		Details: map[string]any{"required_scope": string(scope)},
	}})
}

// How a whoami caller authenticated (whoamiResponse.Via).
const (
	viaToken   = "token"
	viaSession = authn.ViaSession
	// viaNone is a private instance's caller: no credential at all.
	viaNone = "none"
)

// whoamiResponse is the body of GET /api/v1/whoami.
type whoamiResponse struct {
	Principal string        `json:"principal"`
	Name      string        `json:"name,omitempty"`
	Provider  string        `json:"provider"`
	Roles     []string      `json:"roles"`
	Scopes    []authn.Scope `json:"scopes"`
	// Via is how the caller authenticated: token, session (a web UI
	// browser session) or none (private instance).
	Via string `json:"via"`
	// Session describes the browser session of a session caller.
	Session *sessionInfo `json:"session,omitempty"`
}

// Whoami handles GET /api/v1/whoami: the caller's principal, roles and
// effective scopes, how it authenticated and, for a web UI browser
// session, the session itself (sessions, nil-safe, supplies its
// times). A private instance (authEnabled false) reports
// authn.LocalPrincipal, which holds every scope.
func Whoami(authEnabled bool, sessions *authn.Sessions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		via := viaToken
		p, ok := authn.FromContext(r.Context())
		if !ok {
			if authEnabled {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
				return
			}
			p, via = authn.LocalPrincipal(), viaNone
		}
		WriteJSON(w, http.StatusOK, whoamiFor(r, p, via, sessions))
	}
}

// whoamiFor builds the whoami body of p.
func whoamiFor(r *http.Request, p *authn.Principal, via string, sessions *authn.Sessions) whoamiResponse {
	resp := whoamiResponse{
		Principal: p.ID,
		Name:      p.Name,
		Provider:  p.Provider,
		Roles:     append([]string{}, p.Roles...),
		Scopes:    append([]authn.Scope{}, p.Scopes...),
		Via:       via,
	}
	if p.IsSession() {
		resp.Via = viaSession
		if sessions != nil {
			if s, err := sessions.Get(r.Context(), p.Meta[authn.MetaSessionID]); err == nil {
				resp.Session = toSessionInfo(s)
			}
		}
	}
	return resp
}
