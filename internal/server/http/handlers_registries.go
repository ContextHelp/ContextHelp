package http

import (
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// FetchRegistry handles registry manifest fetching.
func FetchRegistry(svc *service.Service) http.HandlerFunc {
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

		if err := svc.FetchRegistry(r.Context(), req.URL); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "fetched",
		})
	}
}

// UpdateRegistry handles registry manual update.
func UpdateRegistry(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := r.PathValue("url")
		if url == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "url is required")
			return
		}

		if err := svc.UpdateRegistry(r.Context(), url); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "updated",
		})
	}
}

// ListRegistries handles registry listing.
func ListRegistries(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		registries, total, err := svc.ListRegistries(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"registries": registries,
			"total":      total,
		})
	}
}
