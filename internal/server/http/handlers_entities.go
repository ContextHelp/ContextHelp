package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListEntities returns a paginated list of entities. With an inbound
// gate wired (non-private instances), the listing is filtered to the
// namespaces the authenticated principal is entitled to — an index
// browse, so no metering charge.
func ListEntities(svc *service.Service, gate *registry.InboundGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := storage.EntityFilter{
			Namespace: r.URL.Query().Get("namespace"),
			Limit:     parseIntDefault(r.URL.Query().Get("limit"), 20),
			Offset:    parseIntDefault(r.URL.Query().Get("offset"), 0),
		}

		entities, err := svc.ListEntities(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if gate != nil {
			principal := gatePrincipal(r)
			entitled := make([]*storage.Entity, 0, len(entities))
			for _, e := range entities {
				if gate.Check(r.Context(), principal, e.Namespace) == nil {
					entitled = append(entitled, e)
				}
			}
			entities = entitled
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data": entities,
		})
	}
}

// GetEntity returns a single entity by slug. A wired inbound gate
// charges the access as a metered entity_resolve against the
// principal's namespace entitlement and quota.
func GetEntity(svc *service.Service, gate *registry.InboundGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		entity, err := svc.GetEntity(r.Context(), slug)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "entity not found")
			return
		}
		if err := gate.Authorize(r.Context(), gatePrincipal(r), entity.Namespace,
			storage.MeteringEventEntityResolve); err != nil {
			if writeInboundGateError(w, r, err) {
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, entity)
	}
}

// EntityBacklinks returns objects that mention the given entity. The
// gate check is unmetered — backlinks ride on the entity's namespace
// entitlement without a quota charge.
func EntityBacklinks(svc *service.Service, gate *registry.InboundGate) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		if gate != nil {
			if entity, err := svc.GetEntity(r.Context(), slug); err == nil {
				if cerr := gate.Check(r.Context(), gatePrincipal(r), entity.Namespace); cerr != nil {
					if writeInboundGateError(w, r, cerr) {
						return
					}
					WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", cerr.Error())
					return
				}
			}
		}
		objs, err := svc.EntityBacklinks(r.Context(), slug)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data": objs,
		})
	}
}
