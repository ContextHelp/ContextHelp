package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// GetObject returns a single object by ID.
func GetObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		obj, err := svc.GetObject(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
			return
		}
		WriteJSON(w, http.StatusOK, obj)
	}
}

// ListObjects returns a paginated list of objects.
func ListObjects(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter := storage.ObjectFilter{
			Type:    r.URL.Query().Get("type"),
			Subtype: r.URL.Query().Get("subtype"),
			Limit:   parseIntDefault(r.URL.Query().Get("limit"), 20),
			Offset:  parseIntDefault(r.URL.Query().Get("offset"), 0),
		}

		objs, total, err := svc.ListObjects(r.Context(), filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"data":  objs,
			"total": total,
		})
	}
}

// UpdateObject updates an object by ID.
func UpdateObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		obj, err := svc.GetObject(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
			return
		}

		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}

		if v, ok := patch["type"].(string); ok {
			obj.Type = v
		}
		if v, ok := patch["subtype"].(string); ok {
			obj.Subtype = v
		}

		if err := svc.UpdateObject(r.Context(), obj); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, obj)
	}
}

// DeleteObject deletes an object by ID.
func DeleteObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.DeleteObject(r.Context(), id); err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return v
}
