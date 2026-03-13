package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RunSlackImport handles POST /api/v1/importers/slack/run.
// It enqueues an import.slack pipeline job with the supplied export path config.
//
// Request body:
//
//	{
//	  "profile":    "optional profile name",
//	  "export_dir": "/path/to/unpacked/slack/export",
//	  "channel":    ["general", "dev"],       // optional channel filter
//	  "since":      "2026-01-01T00:00:00Z",   // optional time filter (RFC3339)
//	  "max_items":  1000,                      // optional cap (0 = all)
//	  "dry_run":    false
//	}
func RunSlackImport(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Profile   string     `json:"profile"`
			ExportDir string     `json:"export_dir"`
			Channel   []string   `json:"channel"`
			Since     *time.Time `json:"since,omitempty"`
			MaxItems  int        `json:"max_items"`
			DryRun    bool       `json:"dry_run"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		if req.ExportDir == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "export_dir is required")
			return
		}

		now := time.Now().Truncate(time.Second)
		runID := uuid.New().String()

		meta := map[string]any{
			"slack_export_dir": req.ExportDir,
		}
		if len(req.Channel) > 0 {
			meta["slack_channel_filter"] = req.Channel
		}
		if req.Since != nil {
			meta["slack_since"] = req.Since.UTC().Format(time.RFC3339)
		}
		if req.MaxItems > 0 {
			meta["slack_max_items"] = req.MaxItems
		}
		if req.Profile != "" {
			meta["profile"] = req.Profile
		}

		if req.DryRun {
			WriteJSON(w, http.StatusOK, map[string]any{
				"run_id":   runID,
				"dry_run":  true,
				"status":   "dry_run",
				"pipeline": "import.slack",
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
			Type:       "importer:slack",
			Status:     storage.JobPending,
			Payload:    string(metaJSON),
			Pipeline:   "import.slack",
			Source:     "import:slack",
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
			"pipeline": "import.slack",
			"profile":  req.Profile,
		})
	}
}
