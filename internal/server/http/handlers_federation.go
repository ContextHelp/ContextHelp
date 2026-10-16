package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// FederationPush handles POST /api/v1/federation/push.
// Accepts bulk objects + edges; applies same dedup logic as LocalPusher.
//
// requirePeer models non-private instances (access != private), where
// the push authenticates like every other request, with an API key, and
// that key must be bound to a principal of kind peer (ADR-079 §1): an
// authenticated owner, person or device is refused with 403
// PEER_REQUIRED, so a device's capture key can never push a whole
// corpus in. The route table already requires write:objects, so the
// peer's roles must grant it. A private instance accepts the push as
// it accepts everything.
//
// A body over federation.max_request_bytes is 413 PAYLOAD_TOO_LARGE,
// read no further: the pusher sends an object over its own request cap
// alone, and fails that object, not the target, on a 413.
//
// Response: 200 OK {"accepted": N} where N = count of newly inserted objects.
func FederationPush(svc *service.Service, requirePeer bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if requirePeer {
			p, ok := authn.FromContext(r.Context())
			if !ok || p.Kind != storage.PrincipalKindPeer {
				WriteError(w, http.StatusForbidden, "PEER_REQUIRED",
					"federation push needs the API key of a peer principal")
				return
			}
		}

		limit := svc.Cfg.Federation.RequestLimit()
		var req federation.FederationPushRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(&req); err != nil {
			if tooLarge := (*http.MaxBytesError)(nil); errors.As(err, &tooLarge) {
				WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", fmt.Sprintf(
					"push body over %d bytes (federation.max_request_bytes)", limit))
				return
			}
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
