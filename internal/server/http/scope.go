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
				writeInsufficientScope(w, scope)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeInsufficientScope(w http.ResponseWriter, scope authn.Scope) {
	w.Header().Set("WWW-Authenticate",
		fmt.Sprintf(`Bearer realm="dpkms", error="insufficient_scope", scope=%q`, string(scope)))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{Error: ErrorBody{
		Code:    CodeInsufficientScope,
		Message: fmt.Sprintf("missing scope %s", scope),
		Details: map[string]any{"required_scope": string(scope)},
	}})
}

// whoamiResponse is the body of GET /api/v1/whoami.
type whoamiResponse struct {
	Principal string        `json:"principal"`
	Name      string        `json:"name,omitempty"`
	Provider  string        `json:"provider"`
	Roles     []string      `json:"roles"`
	Scopes    []authn.Scope `json:"scopes"`
}

// Whoami handles GET /api/v1/whoami: the caller's principal, roles and
// effective scopes. A private instance (authEnabled false) reports
// authn.LocalPrincipal, which holds every scope.
func Whoami(authEnabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := authn.FromContext(r.Context())
		if !ok {
			if authEnabled {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required")
				return
			}
			p = authn.LocalPrincipal()
		}
		resp := whoamiResponse{
			Principal: p.ID,
			Name:      p.Name,
			Provider:  p.Provider,
			Roles:     append([]string{}, p.Roles...),
			Scopes:    append([]authn.Scope{}, p.Scopes...),
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}
