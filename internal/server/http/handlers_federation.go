package http

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// FederationPush handles POST /api/v1/federation/push.
// Accepts bulk objects + edges; applies same dedup logic as LocalPusher.
//
// requireCredential models non-private instances (access != private),
// where a federation credential is MANDATORY: with no federation.token
// configured the route refuses every request (403 FEDERATION_DISABLED)
// — an authenticated principal alone is never enough to push. When a
// token is configured it is always enforced (private instances too),
// compared in constant time.
//
// Response: 200 OK {"accepted": N} where N = count of newly inserted objects.
func FederationPush(svc *service.Service, requireCredential bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverToken := svc.Cfg.Federation.Token
		if requireCredential && serverToken == "" {
			WriteError(w, http.StatusForbidden, "FEDERATION_DISABLED",
				"federation push requires a configured federation.token on non-private instances")
			return
		}
		if serverToken != "" {
			auth := r.Header.Get("Authorization")
			if subtle.ConstantTimeCompare([]byte(auth), []byte("Bearer "+serverToken)) != 1 {
				WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing federation token")
				return
			}
		}

		var req federation.FederationPushRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}

		n, err := svc.FederationAccept(r.Context(), req.Objects, req.Edges, req.Entities)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]int{"accepted": n})
	}
}
