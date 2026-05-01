package http

import (
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// FederationPush handles POST /api/v1/federation/push.
// Accepts bulk objects + edges; applies same dedup logic as LocalPusher.
// Optional Bearer token auth: if server has federation.token configured, validate it.
// Response: 200 OK {"accepted": N} where N = count of newly inserted objects.
func FederationPush(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth: check Bearer token if configured.
		serverToken := svc.Cfg.Federation.Token
		if serverToken != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+serverToken {
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
