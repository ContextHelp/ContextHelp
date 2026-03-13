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
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
)

// CreateWatch handles POST /api/v1/watches.
func CreateWatch(svc *service.Service, mgr *watcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Path             string   `json:"path"`
			Mode             string   `json:"mode"`
			IncludePatterns  []string `json:"include_patterns"`
			ExcludePatterns  []string `json:"exclude_patterns"`
			DebounceMS       int      `json:"debounce_ms"`
			PipelineOverride string   `json:"pipeline_override"`
			Paused           bool     `json:"paused"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		if req.Path == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "path is required")
			return
		}
		if req.Mode == "" {
			req.Mode = watcher.DetectMode(req.Path)
		}
		if req.DebounceMS == 0 {
			req.DebounceMS = 500
		}
		watchStatus := "active"
		if req.Paused {
			watchStatus = "paused"
		}
		now := time.Now()
		cfg := &storage.WatchConfig{
			ID:               uuid.New().String(),
			Path:             req.Path,
			Mode:             req.Mode,
			IncludePatterns:  req.IncludePatterns,
			ExcludePatterns:  req.ExcludePatterns,
			DebounceMS:       req.DebounceMS,
			Status:           watchStatus,
			PipelineOverride: req.PipelineOverride,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if mgr != nil {
			if err := mgr.AddWatch(r.Context(), cfg); err != nil {
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
		} else {
			if err := svc.Store.Watches().CreateWatch(r.Context(), cfg); err != nil {
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
		}
		WriteJSON(w, http.StatusCreated, cfg)
	}
}

// ListWatches handles GET /api/v1/watches.
func ListWatches(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		watchStatus := r.URL.Query().Get("status")
		watches, err := svc.Store.Watches().ListWatches(r.Context(), watchStatus)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"watches": watches, "total": len(watches)})
	}
}

// GetWatch handles GET /api/v1/watches/{id}.
func GetWatch(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		cfg, err := svc.Store.Watches().GetWatch(r.Context(), id)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "NOT_FOUND", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg)
	}
}

// UpdateWatch handles PATCH /api/v1/watches/{id}.
func UpdateWatch(svc *service.Service, mgr *watcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		cfg, err := svc.Store.Watches().GetWatch(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
			return
		}
		var patch struct {
			IncludePatterns  *[]string `json:"include_patterns"`
			ExcludePatterns  *[]string `json:"exclude_patterns"`
			DebounceMS       *int      `json:"debounce_ms"`
			PipelineOverride *string   `json:"pipeline_override"`
		}
		json.NewDecoder(r.Body).Decode(&patch)
		if patch.IncludePatterns != nil {
			cfg.IncludePatterns = *patch.IncludePatterns
		}
		if patch.ExcludePatterns != nil {
			cfg.ExcludePatterns = *patch.ExcludePatterns
		}
		if patch.DebounceMS != nil {
			cfg.DebounceMS = *patch.DebounceMS
		}
		if patch.PipelineOverride != nil {
			cfg.PipelineOverride = *patch.PipelineOverride
		}
		cfg.UpdatedAt = time.Now()
		if err := svc.Store.Watches().UpdateWatch(r.Context(), cfg); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, cfg)
	}
}

// DeleteWatch handles DELETE /api/v1/watches/{id}.
func DeleteWatch(svc *service.Service, mgr *watcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var deleteErr error
		if mgr != nil {
			deleteErr = mgr.RemoveWatch(r.Context(), id)
		} else {
			deleteErr = svc.Store.Watches().DeleteWatch(r.Context(), id)
		}
		if deleteErr != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(deleteErr.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", deleteErr.Error())
			return
		}
		WriteJSON(w, http.StatusNoContent, nil)
	}
}

// PauseWatch handles POST /api/v1/watches/{id}/pause.
func PauseWatch(mgr *watcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			WriteError(w, http.StatusServiceUnavailable, "NO_MANAGER", "watcher manager not available")
			return
		}
		id := chi.URLParam(r, "id")
		if err := mgr.PauseWatch(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "paused"})
	}
}

// ResumeWatch handles POST /api/v1/watches/{id}/resume.
func ResumeWatch(mgr *watcher.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			WriteError(w, http.StatusServiceUnavailable, "NO_MANAGER", "watcher manager not available")
			return
		}
		id := chi.URLParam(r, "id")
		if err := mgr.ResumeWatch(r.Context(), id); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "active"})
	}
}

// ListWatchFiles handles GET /api/v1/watches/{id}/files.
func ListWatchFiles(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		recs, err := svc.Store.Watches().ListFileRecords(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"files": recs, "total": len(recs)})
	}
}
