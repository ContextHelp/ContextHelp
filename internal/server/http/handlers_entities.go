package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListEntities returns a paginated list of entities.
func ListEntities(svc *service.Service) http.HandlerFunc {
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
		WriteJSON(w, http.StatusOK, map[string]any{
			"data": entities,
		})
	}
}

// GetEntity returns a single entity by slug.
func GetEntity(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
		entity, err := svc.GetEntity(r.Context(), slug)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "entity not found")
			return
		}
		WriteJSON(w, http.StatusOK, entity)
	}
}

// EntityBacklinks returns objects that mention the given entity.
func EntityBacklinks(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "slug")
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
