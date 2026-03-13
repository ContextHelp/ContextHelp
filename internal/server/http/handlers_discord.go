package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RunDiscordImport handles POST /api/v1/importers/discord/run.
// It enqueues an import.discord pipeline job with the supplied export file config.
//
// Request body:
//
//	{
//	  "profile":     "optional profile name",
//	  "export_file": "/path/to/discord/export.json",
//	  "since":       "2026-01-01T00:00:00Z",   // optional time filter (RFC3339)
//	  "max_items":   1000,                       // optional cap (0 = all)
//	  "dry_run":     false
//	}
func RunDiscordImport(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Profile    string     `json:"profile"`
			ExportFile string     `json:"export_file"`
			Since      *time.Time `json:"since,omitempty"`
			MaxItems   int        `json:"max_items"`
			DryRun     bool       `json:"dry_run"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		if req.ExportFile == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "export_file is required")
			return
		}

		now := time.Now().Truncate(time.Second)
		runID := uuid.New().String()

		meta := map[string]any{
			"discord_export_file": req.ExportFile,
		}
		if req.Since != nil {
			meta["discord_since"] = req.Since.UTC().Format(time.RFC3339)
		}
		if req.MaxItems > 0 {
			meta["discord_max_items"] = req.MaxItems
		}
		if req.Profile != "" {
			meta["profile"] = req.Profile
		}

		if req.DryRun {
			WriteJSON(w, http.StatusOK, map[string]any{
				"run_id":   runID,
				"dry_run":  true,
				"status":   "dry_run",
				"pipeline": "import.discord",
				"profile":  req.Profile,
			})
			return
		}

		metaJSON, err := json.Marshal(meta)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "encode metadata: "+err.Error())
			return
		}

		job := &storage.Job{
			ID:         runID,
			Type:       "importer:discord",
			Status:     storage.JobPending,
			Payload:    string(metaJSON),
			Pipeline:   "import.discord",
			Source:     "import:discord",
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
			"pipeline": "import.discord",
			"profile":  req.Profile,
		})
	}
}
