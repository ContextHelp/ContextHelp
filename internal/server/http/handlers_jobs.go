package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListJobs returns a paginated list of jobs.
func ListJobs(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := storage.JobFilter{
			Status: storage.JobStatus(r.URL.Query().Get("status")),
			Limit:  parseIntDefault(r.URL.Query().Get("limit"), 20),
			Offset: parseIntDefault(r.URL.Query().Get("offset"), 0),
		}

		list, total, err := svc.ListJobs(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if list == nil {
			list = []*storage.Job{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data":  list,
			"total": total,
		})
	}
}

// GetJob returns a single job by ID.
func GetJob(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		job, err := svc.GetJob(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "job not found")
			return
		}
		WriteJSON(w, http.StatusOK, job)
	}
}

// RetryJob retries a failed job.
func RetryJob(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.RetryJob(r.Context(), id); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		}

		job, err := svc.GetJob(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "job not found")
			return
		}
		WriteJSON(w, http.StatusOK, job)
	}
}
