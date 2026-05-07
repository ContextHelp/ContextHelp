package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// CaptureInbox handles POST /api/v1/inbox.
func CaptureInbox(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content   string   `json:"content"`
			Type      string   `json:"type"`
			Source    string   `json:"source"`
			InboxNote string   `json:"inbox_note"`
			Hints     []string `json:"hints"`
			Mentions  []string `json:"mentions"`
			Profile   string   `json:"profile"` // T-0588
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		// Support form encoding from PWA Web Share Target.
		if req.Content == "" {
			req.Content = r.FormValue("content")
			if req.Content == "" {
				req.Content = r.FormValue("text")
			}
		}
		if req.Content == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "content is required")
			return
		}
		captureReq := service.InboxCaptureRequest{
			Content:   req.Content,
			Type:      req.Type,
			Source:    req.Source,
			InboxNote: req.InboxNote,
			Hints:     req.Hints,
			Mentions:  mentions.ParseSlice(req.Mentions),
			Profile:   req.Profile,
		}
		obj, err := svc.CaptureToInbox(r.Context(), captureReq)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusCreated, obj)
	}
}

// ListInbox handles GET /api/v1/inbox.
func ListInbox(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 {
			limit = 20
		}
		offset, _ := strconv.Atoi(q.Get("offset"))

		filter := service.InboxFilter{
			Limit:  limit,
			Offset: offset,
		}
		if b := q.Get("before"); b != "" {
			if t, err := time.Parse(time.RFC3339, b); err == nil {
				filter.Before = t
			}
		}
		if a := q.Get("after"); a != "" {
			if t, err := time.Parse(time.RFC3339, a); err == nil {
				filter.After = t
			}
		}

		items, total, err := svc.ListInbox(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
	}
}

// TriageInbox handles POST /api/v1/inbox/{id}/triage.
func TriageInbox(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req service.TriageRequest
		json.NewDecoder(r.Body).Decode(&req)

		jobID, err := svc.TriageInbox(r.Context(), id, req)
		if err != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"job_id": jobID})
	}
}

// DiscardInbox handles POST /api/v1/inbox/{id}/discard.
func DiscardInbox(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.DiscardInbox(r.Context(), id); err != nil {
			statusCode := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				statusCode = http.StatusNotFound
			}
			WriteError(w, statusCode, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusNoContent, nil)
	}
}

// ManifestJSON serves the PWA web app manifest at /manifest.json (Web Share Target support).
func ManifestJSON() http.HandlerFunc {
	const manifest = `{
  "name": "ctxt — Knowledge Management",
  "short_name": "ctxt",
  "description": "Local-first knowledge management interface",
  "start_url": "/ui",
  "display": "standalone",
  "orientation": "any",
  "theme_color": "#1a2332",
  "background_color": "#fdfcf9",
  "icons": [
    {"src": "/ui/icons/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "maskable any"},
    {"src": "/ui/icons/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable any"}
  ],
  "categories": ["productivity", "utilities"],
  "share_target": {
    "action": "/api/v1/inbox",
    "method": "POST",
    "enctype": "application/x-www-form-urlencoded",
    "params": {"title": "title", "text": "text", "url": "url"}
  }
}`
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/manifest+json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(manifest)) //nolint:errcheck
	}
}
