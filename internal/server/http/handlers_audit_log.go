package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListAuditLog handles GET /api/v1/audit-log
func ListAuditLog(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		filter := storage.AuditFilter{
			ObjectID: q.Get("object_id"),
			Actor:    q.Get("actor"),
			Limit:    parseIntDefault(q.Get("limit"), 20),
			Offset:   parseIntDefault(q.Get("offset"), 0),
		}

		// Support comma-separated event types.
		if types := q.Get("type"); types != "" {
			parts := strings.Split(types, ",")
			if len(parts) == 1 {
				filter.EventType = parts[0]
			} else {
				filter.EventTypes = parts
			}
		}

		if since := q.Get("since"); since != "" {
			t, err := time.Parse("2006-01-02", since)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "INVALID_PARAM",
					"since must be YYYY-MM-DD")
				return
			}
			filter.After = t
		}

		if until := q.Get("until"); until != "" {
			t, err := time.Parse("2006-01-02", until)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "INVALID_PARAM",
					"until must be YYYY-MM-DD")
				return
			}
			// Include the full day.
			filter.Before = t.Add(24*time.Hour - time.Nanosecond)
		}

		entries, total, err := svc.Store.AuditLog().List(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError,
				"INTERNAL_ERROR", err.Error())
			return
		}
		if entries == nil {
			entries = []*storage.AuditEntry{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data":  entries,
			"total": total,
		})
	}
}
