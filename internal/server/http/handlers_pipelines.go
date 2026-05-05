package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CreatePipeline handles pipeline creation.
func CreatePipeline(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.CreatePipelineRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}

		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if req.Steps == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "steps is required")
			return
		}

		id, err := svc.CreatePipeline(withPolicyContext(r), req)
		if err != nil {
			if writePolicyError(w, err) {
				return
			}
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "parse steps") || strings.Contains(err.Error(), "invalid JSON") {
				statusCode = http.StatusBadRequest
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusCreated, map[string]string{
			"id": id,
		})
	}
}

// ListPipelines handles pipeline listing.
func ListPipelines(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := storage.PipelineFilter{
			Name:            r.URL.Query().Get("name"),
			IncludeArchived: r.URL.Query().Get("include_archived") == "true",
			OnlyArchived:    r.URL.Query().Get("only_archived") == "true",
		}

		pipelines, total, err := svc.ListPipelines(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"pipelines": pipelines,
			"total":     total,
		})
	}
}

// GetPipeline handles pipeline retrieval.
func GetPipeline(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		pipeline, err := svc.GetPipeline(r.Context(), name)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "pipeline not found")
			return
		}

		WriteJSON(w, http.StatusOK, pipeline)
	}
}

// DeletePipeline handles pipeline deletion.
func DeletePipeline(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if err := svc.DeletePipeline(withPolicyContext(r), name); err != nil {
			if writePolicyError(w, err) {
				return
			}
			statusCode := http.StatusInternalServerError
			errCode := "INTERNAL_ERROR"
			if strings.Contains(err.Error(), "PROTECTED") || strings.Contains(err.Error(), "built-in") {
				statusCode = http.StatusForbidden
				errCode = "PROTECTED_PIPELINE"
			} else if strings.Contains(err.Error(), "no rows") || strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
				errCode = "NOT_FOUND"
			}
			WriteError(w, statusCode, errCode, err.Error())
			return
		}

		WriteJSON(w, http.StatusNoContent, nil)
	}
}

// ArchivePipeline handles pipeline archiving.
func ArchivePipeline(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if err := svc.ArchivePipeline(withPolicyContext(r), name); err != nil {
			if writePolicyError(w, err) {
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "archived",
		})
	}
}

// UnarchivePipeline handles pipeline unarchiving.
func UnarchivePipeline(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if err := svc.UnarchivePipeline(withPolicyContext(r), name); err != nil {
			if writePolicyError(w, err) {
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "unarchived",
		})
	}
}

// Enqueue handles unified enqueue endpoint.
func Enqueue(svc *service.Service) http.HandlerFunc {
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

		jobID, err := svc.Enqueue(r.Context(), req)
		if err != nil {
			if errors.Is(err, jobs.ErrPipelineNotFound) {
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
