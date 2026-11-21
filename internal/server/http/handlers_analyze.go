package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// analyzeRequestBody is the request body bounded by the instance's analyze
// limit (service.AnalyzeBodyLimit); unbounded when there is none.
func analyzeRequestBody(w http.ResponseWriter, r *http.Request, svc *service.Service) io.Reader {
	if limit := svc.AnalyzeBodyLimit(); limit > 0 {
		return http.MaxBytesReader(w, r.Body, limit)
	}
	return r.Body
}

// Analyze handles content analysis requests.
func Analyze(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.AnalyzeRequest
		if err := json.NewDecoder(analyzeRequestBody(w, r, svc)).Decode(&req); err != nil {
			if tooLarge := (*http.MaxBytesError)(nil); errors.As(err, &tooLarge) {
				WriteError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body over the analyze limit")
				return
			}
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
			if errors.Is(err, service.ErrInvalidContentEncoding) {
				WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
				return
			}
			// surface unsupported types as 422 so callers see a
			// clear, actionable error instead of a generic 500. Previously
			// this path silently enqueued a job referencing a non-existent
			// pipeline and the worker dropped the job.
			if errors.Is(err, service.ErrPipelineNotFound) {
				WriteError(w, http.StatusUnprocessableEntity, "PIPELINE_NOT_FOUND", err.Error())
				return
			}
			if errors.Is(err, service.ErrPipelineArchived) {
				WriteError(w, http.StatusConflict, "CONFLICT", err.Error())
				return
			}
			// a note or data over its limit is the caller's to fix.
			if errors.Is(err, service.ErrInvalidSubmission) {
				WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
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
