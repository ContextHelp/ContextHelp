package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ListHolds handles GET /api/v1/holds.
func ListHolds(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		filter := storage.HoldFilter{
			ObjectID:  q.Get("object_id"),
			SourceKey: q.Get("source_key"),
			HoldID:    q.Get("hold_id"),
			Source:    q.Get("source"),
			Limit:     parseIntDefault(q.Get("limit"), 100),
			Offset:    parseIntDefault(q.Get("offset"), 0),
		}
		holds, err := svc.ListHolds(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if holds == nil {
			holds = []storage.ObjectHold{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": holds, "total": len(holds)})
	}
}

// PlaceHold handles POST /api/v1/holds.
func PlaceHold(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.PlaceHoldOptions
		if !decodeStrict(w, r, &req) {
			return
		}
		placed, err := svc.PlaceHold(r.Context(), req)
		switch {
		case errors.Is(err, service.ErrInvalidHold):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case errors.Is(err, storage.ErrNotFound):
			WriteError(w, http.StatusNotFound, CodeNotFound, "object not found")
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"data": placed, "count": len(placed)})
	}
}

// ReleaseHold handles DELETE /api/v1/holds/{hold_id}.
func ReleaseHold(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		holdID := chi.URLParam(r, "hold_id")
		q := r.URL.Query()
		released, err := svc.ReleaseHold(r.Context(), service.ReleaseHoldOptions{
			HoldID:    holdID,
			ObjectID:  q.Get("object_id"),
			SourceKey: q.Get("source_key"),
			Source:    q.Get("source"),
			Reason:    q.Get("reason"),
		})
		switch {
		case errors.Is(err, service.ErrInvalidHold):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case errors.Is(err, service.ErrHoldAuthority):
			WriteError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"hold_id": holdID, "released": released})
	}
}
