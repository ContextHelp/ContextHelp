package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListPendingEntities lists the approval queue: entities untrusted
// registries served, not written yet.
// GET /api/v1/entities/registry-pending?registry=<name>
func ListPendingEntities(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pending, err := svc.ListPendingEntities(r.Context(), r.URL.Query().Get("registry"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"pending": service.RedactPending(pending),
			"total":   len(pending),
		})
	}
}

// pendingSelectionRequest is the body of approve and reject: ids, or
// all of one registry's queued entities.
type pendingSelectionRequest struct {
	IDs      []string `json:"ids"`
	All      bool     `json:"all"`
	Registry string   `json:"registry"`
}

// ApprovePendingEntities writes the selected queued entities and drops
// them from the queue.
// POST /api/v1/entities/registry-pending/approve
func ApprovePendingEntities(svc *service.Service) http.HandlerFunc {
	return pendingAction(svc.ApprovePendingEntities, "approved")
}

// RejectPendingEntities drops the selected queued entities unwritten.
// POST /api/v1/entities/registry-pending/reject
func RejectPendingEntities(svc *service.Service) http.HandlerFunc {
	return pendingAction(svc.RejectPendingEntities, "rejected")
}

func pendingAction(
	act func(ctx context.Context, sel service.PendingSelection) ([]*storage.PendingEntity, error),
	key string,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req pendingSelectionRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		done, err := act(r.Context(), service.PendingSelection{IDs: req.IDs, All: req.All, Registry: req.Registry})
		switch {
		case errors.Is(err, service.ErrPendingSelection):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		case errors.Is(err, storage.ErrNotFound):
			WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		case errors.Is(err, service.ErrPendingSandboxed):
			WriteError(w, http.StatusConflict, "REGISTRY_SANDBOXED", err.Error())
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		default:
			WriteJSON(w, http.StatusOK, map[string]any{key: service.RedactPending(done), "total": len(done)})
		}
	}
}
