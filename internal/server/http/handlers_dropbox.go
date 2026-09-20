package http

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RunDropboxImport handles POST /api/v1/importers/dropbox/run.
// It enqueues a dropbox.sync pipeline job with the supplied profile config.
func RunDropboxImport(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Profile     string     `json:"profile"`
			AccessToken string     `json:"access_token"`
			Path        string     `json:"path"`
			Cursor      string     `json:"cursor"`
			Recursive   bool       `json:"recursive"`
			MaxItems    int        `json:"max_items"`
			DryRun      bool       `json:"dry_run"`
			Since       *time.Time `json:"since,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		if strings.TrimSpace(req.AccessToken) == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "access_token is required")
			return
		}

		now := time.Now().Truncate(time.Second)
		runID := uuid.New().String()

		meta := map[string]any{
			"dropbox_token":     req.AccessToken,
			"dropbox_path":      req.Path,
			"dropbox_recursive": req.Recursive,
		}
		if req.Cursor != "" {
			meta["dropbox_cursor"] = req.Cursor
		}
		if req.MaxItems > 0 {
			meta["dropbox_max_items"] = req.MaxItems
		}

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "encode metadata: "+err.Error())
			return
		}

		if req.DryRun {
			WriteJSON(w, http.StatusOK, map[string]any{
				"run_id":   runID,
				"dry_run":  true,
				"status":   "dry_run",
				"pipeline": "dropbox.sync",
				"profile":  req.Profile,
			})
			return
		}

		job := &storage.Job{
			ID:         runID,
			Type:       "importer:dropbox",
			Status:     storage.JobPending,
			Payload:    string(metaJSON),
			Pipeline:   "dropbox.sync",
			Source:     "import:dropbox",
			MaxRetries: 3,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := svc.Store.Jobs().Create(r.Context(), job); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, map[string]any{
			"run_id":   runID,
			"status":   string(storage.JobPending),
			"pipeline": "dropbox.sync",
			"profile":  req.Profile,
		})
	}
}

// GetImporterRun handles GET /api/v1/importers/runs/{id}.
// Returns the job status for an importer run.
func GetImporterRun(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		job, err := svc.Store.Jobs().Get(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "run not found")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"run_id":     job.ID,
			"status":     string(job.Status),
			"pipeline":   job.Pipeline,
			"source":     job.Source,
			"error":      job.Error,
			"created_at": job.CreatedAt,
			"updated_at": job.UpdatedAt,
		})
	}
}
