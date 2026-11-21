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
			writeSearchStateError(w, err, "search history entries")
			return
		}
		if results == nil {
			results = []*storage.SearchHistoryEntry{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": results})
	}
}

// ClearSearchHistory handles DELETE /api/v1/search-history: the caller's
// own history, one profile's with profile_id, otherwise all of it.
func ClearSearchHistory(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profileID := r.URL.Query().Get("profile_id")
		if err := svc.ClearSearchHistory(r.Context(), profileID); err != nil {
			writeSearchStateError(w, err, "search history entries")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
