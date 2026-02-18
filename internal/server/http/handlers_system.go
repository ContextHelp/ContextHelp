package http

import (
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// ListReminders handles system reminder listing.
func ListReminders(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		activeOnly := r.URL.Query().Get("active_only") == "true"

		reminders, total, err := svc.ListReminders(r.Context(), activeOnly)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"reminders": reminders,
			"total":     total,
		})
	}
}

// DismissReminder handles reminder dismissal.
func DismissReminder(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "id is required")
			return
		}

		if err := svc.DismissReminder(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "dismissed",
		})
	}
}
