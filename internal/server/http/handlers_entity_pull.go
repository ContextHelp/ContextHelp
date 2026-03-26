package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// PullEntity promotes a thin entity to full by fetching its definition on demand.
// POST /api/v1/entities/{slug}/pull
func PullEntity(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		if slug == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "slug is required")
			return
		}

		entity, err := svc.PullEntity(r.Context(), slug)
		if err != nil {
			if errors.Is(err, storage.ErrEntityDefinitionUnavailable) {
				WriteError(w, http.StatusNotFound, "DEFINITION_UNAVAILABLE", err.Error())
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, entity)
	}
}

// SyncRegistryEntities triggers an entity index sync for a registry (thin or full).
// POST /api/v1/entities/registry-sync
func SyncRegistryEntities(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			URL string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		if req.URL == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "url is required")
			return
		}

		count, err := svc.SyncRegistryEntities(r.Context(), req.URL)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"status":   "synced",
			"upserted": count,
		})
	}
}
