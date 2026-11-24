package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/expiry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SetObjectExpiry handles PUT /api/v1/objects/{id}/expiry.
func SetObjectExpiry(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			ExpiresAt   *time.Time `json:"expires_at,omitempty"`
			TTL         string     `json:"ttl,omitempty"`
			ExpiryScope string     `json:"expiry_scope,omitempty"`
			Scope       string     `json:"scope,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		var at time.Time
		switch {
		case req.ExpiresAt != nil && !req.ExpiresAt.IsZero():
			at = req.ExpiresAt.UTC()
		case strings.TrimSpace(req.TTL) != "":
			d, err := expiry.ParseTTL(req.TTL)
			if err != nil {
				WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
				return
			}
			at = time.Now().UTC().Add(d).Truncate(time.Second)
		default:
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "expires_at or ttl is required")
			return
		}
		scope := strings.TrimSpace(req.ExpiryScope)
		if scope == "" {
			scope = strings.TrimSpace(req.Scope)
		}
		if err := svc.SetObjectExpiry(r.Context(), id, at, scope); err != nil {
			switch {
			case errors.Is(err, storage.ErrObjectHeld):
				WriteError(w, http.StatusConflict, "OBJECT_HELD", err.Error())
			case errors.Is(err, storage.ErrNotFound):
				WriteError(w, http.StatusNotFound, CodeNotFound, err.Error())
			case errors.Is(err, service.ErrInvalidSubmission):
				WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			default:
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			}
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"id":           id,
			"expires_at":   at.Format(time.RFC3339),
			"expiry_scope": scope,
		})
	}
}

// ClearObjectExpiry handles DELETE /api/v1/objects/{id}/expiry.
func ClearObjectExpiry(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.ClearObjectExpiry(r.Context(), id); err != nil {
			switch {
			case errors.Is(err, storage.ErrObjectHeld):
				WriteError(w, http.StatusConflict, "OBJECT_HELD", err.Error())
			case errors.Is(err, storage.ErrNotFound):
				WriteError(w, http.StatusNotFound, CodeNotFound, err.Error())
			default:
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			}
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"id": id, "cleared": true})
	}
}
