package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// PullEntity promotes a thin entity to full by fetching its definition on demand.
// POST /api/v1/entities/{slug}/pull
//
// A wired inbound gate charges the access as a metered content_pull
// against the principal's namespace entitlement and quota before the
// definition fetch runs.
func PullEntity(svc *service.Service, gate *registry.InboundGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		if slug == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "slug is required")
			return
		}
		// An entity the store cannot resolve fails CLOSED: a lookup
		// error never skips the gate.
		if gate != nil {
			existing, err := svc.GetEntity(r.Context(), slug)
			if err != nil || existing == nil {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "entity not found")
				return
			}
			if aerr := gate.Authorize(r.Context(), gatePrincipal(r), existing.Namespace,
				storage.MeteringEventContentPull); aerr != nil {
				if writeInboundGateError(w, r, aerr) {
					return
				}
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", aerr.Error())
				return
			}
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
