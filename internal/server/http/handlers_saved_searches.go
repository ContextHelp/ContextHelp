package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CreateSavedSearch handles POST /api/v1/saved-searches
func CreateSavedSearch(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name      string `json:"name"`
			Query     string `json:"query"`
			ProfileID string `json:"profile_id"`
			AlertOn   string `json:"alert_on"`
			Notify    string `json:"notify"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
			return
		}
		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}
		if req.Query == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "query is required")
			return
		}
		ss, err := svc.CreateSavedSearch(r.Context(), service.SavedSearchCreateRequest{
			Name:      req.Name,
			Query:     req.Query,
			ProfileID: req.ProfileID,
			AlertOn:   req.AlertOn,
			Notify:    req.Notify,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusCreated, ss)
	}
}

// ListSavedSearches handles GET /api/v1/saved-searches
func ListSavedSearches(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := storage.SavedSearchFilter{
			ProfileID: q.Get("profile_id"),
			Limit:     parseIntDefault(q.Get("limit"), 50),
			Offset:    parseIntDefault(q.Get("offset"), 0),
		}
		results, err := svc.ListSavedSearches(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if results == nil {
			results = []*storage.SavedSearch{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": results})
	}
}

// GetSavedSearch handles GET /api/v1/saved-searches/{name}
func GetSavedSearch(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		ss, err := svc.GetSavedSearch(r.Context(), name)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if ss == nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "saved search not found")
			return
		}
		WriteJSON(w, http.StatusOK, ss)
	}
}

// DeleteSavedSearch handles DELETE /api/v1/saved-searches/{name}
func DeleteSavedSearch(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if err := svc.DeleteSavedSearch(r.Context(), name); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
