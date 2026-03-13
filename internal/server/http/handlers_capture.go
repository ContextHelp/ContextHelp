package http

import (
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// capturePageRequest is the body for POST /api/v1/capture/page.
type capturePageRequest struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Pipeline  string `json:"pipeline,omitempty"`
	AuthState string `json:"auth_state,omitempty"`
}

// captureSelectionRequest is the body for POST /api/v1/capture/selection.
type captureSelectionRequest struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Selection string `json:"selection"`
	Pipeline  string `json:"pipeline,omitempty"`
	AuthState string `json:"auth_state,omitempty"`
}

// captureElementRequest is the body for POST /api/v1/capture/element.
type captureElementRequest struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Selector  string `json:"selector"`
	Content   string `json:"content"`
	Pipeline  string `json:"pipeline,omitempty"`
	AuthState string `json:"auth_state,omitempty"`
}

// CapturePage handles POST /api/v1/capture/page.
// Captures the full text of a web page.
func CapturePage(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req capturePageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.URL == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_URL", "url is required")
			return
		}

		content := req.Content
		if content == "" {
			content = req.URL
		}
		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "url.generic"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     content,
			Type:        "url",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// CaptureSelection handles POST /api/v1/capture/selection.
// Captures selected text from a web page.
func CaptureSelection(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req captureSelectionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.Selection == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_SELECTION", "selection is required")
			return
		}

		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "text.default"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     req.Selection,
			Type:        "text",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// CaptureElement handles POST /api/v1/capture/element.
// Captures the text content of a specific DOM element.
func CaptureElement(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req captureElementRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.Content == "" {
			WriteError(w, http.StatusBadRequest, "MISSING_CONTENT", "content is required")
			return
		}

		pipeline := req.Pipeline
		if pipeline == "" {
			pipeline = "text.default"
		}

		jobID, err := svc.Analyze(r.Context(), service.AnalyzeRequest{
			Content:     req.Content,
			Type:        "text",
			Pipeline:    pipeline,
			Source:      req.URL,
			SourceTitle: req.Title,
			AuthState:   req.AuthState,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "ANALYZE_FAILED", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
	}
}

// ListRecentCaptures handles GET /api/v1/capture/recent.
// Returns the last 10 jobs whose source originates from the extension.
func ListRecentCaptures(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		jobs, _, err := svc.ListJobs(r.Context(), storage.JobFilter{
			Limit: 10,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": jobs, "total": len(jobs)})
	}
}
