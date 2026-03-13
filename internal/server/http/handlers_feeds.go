package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CreateFeed handles feed creation.
func CreateFeed(svc *service.Service) http.HandlerFunc {
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
		if !strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://") {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "url must be a valid http/https URL")
			return
		}

		feed, err := svc.CreateFeed(r.Context(), req.URL)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusCreated, feed)
	}
}

// ListFeeds returns all feed subscriptions.
func ListFeeds(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		feeds, err := svc.ListFeeds(r.Context(), storage.FeedFilter{
			Status: r.URL.Query().Get("status"),
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, feeds)
	}
}

// SyncFeed triggers a sync job for a feed.
func SyncFeed(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		jobID, err := svc.SyncFeed(r.Context(), id)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// DeleteFeed deletes a feed subscription.
func DeleteFeed(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.DeleteFeed(r.Context(), id); err != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusNoContent, nil)
	}
}
