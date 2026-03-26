package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Search handles RSQL search queries.
func Search(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "q parameter is required")
			return
		}

		limit := parseIntDefault(r.URL.Query().Get("limit"), 20)
		offset := parseIntDefault(r.URL.Query().Get("offset"), 0)
		profile := r.URL.Query().Get("profile")

		var profileArgs []string
		if profile != "" {
			profileArgs = []string{profile}
		}

		results, total, err := svc.SearchObjects(r.Context(), q, limit, offset, profileArgs...)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"data":  results,
			"total": total,
		})
	}
}
