package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListSearchHistory handles GET /api/v1/search-history
func ListSearchHistory(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := storage.SearchHistoryFilter{
			ProfileID: q.Get("profile_id"),
			Limit:     parseIntDefault(q.Get("limit"), 50),
			Offset:    parseIntDefault(q.Get("offset"), 0),
		}
		results, err := svc.ListSearchHistory(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if results == nil {
			results = []*storage.SearchHistoryEntry{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": results})
	}
}

// ClearSearchHistory handles DELETE /api/v1/search-history
func ClearSearchHistory(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profileID := r.URL.Query().Get("profile_id")
		if profileID == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "profile_id is required")
			return
		}
		if err := svc.ClearSearchHistory(r.Context(), profileID); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
