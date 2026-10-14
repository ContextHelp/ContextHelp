package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// defaultPageLimit is the page size of GET /pages without ?limit=.
const defaultPageLimit = 50

// ListPages handles GET /api/v1/pages: up to ?limit= entity pages
// (objects of type entity_page), newest first.
func ListPages(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, []string{"limit"}); err != nil {
			writeParamError(w, err)
			return
		}
		limit, perr := intParam(q, "limit", defaultPageLimit, 1, 0)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		pages, err := svc.ListEntityPages(r.Context(), limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if pages == nil {
			pages = []*storage.KnowledgeObject{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": pages})
	}
}

// GetPage handles GET /api/v1/pages/{slug}: the entity page of one
// entity slug.
func GetPage(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := svc.GetEntityPage(r.Context(), chi.URLParam(r, "slug"))
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if page == nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "entity page not found")
			return
		}
		WriteJSON(w, http.StatusOK, page)
	}
}

// RefreshPage handles POST /api/v1/pages/{slug}/refresh: recompile the
// page from its current sources, store it and return it.
func RefreshPage(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		page, err := svc.RefreshEntityPage(r.Context(), chi.URLParam(r, "slug"))
		if errors.Is(err, storage.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "entity page not found")
			return
		}
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, page)
	}
}
