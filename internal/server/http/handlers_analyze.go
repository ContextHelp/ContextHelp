package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// Analyze handles content analysis requests.
func Analyze(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.AnalyzeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}

		if req.Content == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "content is required")
			return
		}

		if req.Type == "" {
			req.Type = "text"
		}

		jobID, err := svc.Analyze(r.Context(), req)
		if err != nil {
			// T-0562: surface unsupported types as 422 so callers see a
			// clear, actionable error instead of a generic 500. Previously
			// this path silently enqueued a job referencing a non-existent
			// pipeline and the worker dropped the job.
			if errors.Is(err, service.ErrPipelineNotFound) {
				WriteError(w, http.StatusUnprocessableEntity, "PIPELINE_NOT_FOUND", err.Error())
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{
			"job_id": jobID,
		})
	}
}
