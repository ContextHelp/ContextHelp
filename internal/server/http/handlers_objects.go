package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
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

// writeObjectError answers a failed object operation: 404 for a missing
// object, 500 otherwise.
func writeObjectError(w http.ResponseWriter, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
		return
	}
	WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
}

// decodeStrict decodes a JSON body into v, rejecting unknown fields and
// trailing data. It writes the 400 and returns false on failure.
func decodeStrict(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: "+err.Error())
		return false
	}
	if dec.More() {
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: trailing data")
		return false
	}
	return true
}

// UpdateObject handles PATCH /api/v1/objects/{id}: a partial update of
// type, subtype, title, summary, tags and mentions (service.ObjectPatch).
// An unknown field, no field, or title together with summary is a 400.
func UpdateObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var patch service.ObjectPatch
		if !decodeStrict(w, r, &patch) {
			return
		}
		obj, err := svc.PatchObject(r.Context(), id, patch)
		switch {
		case errors.Is(err, service.ErrInvalidMention):
			WriteError(w, http.StatusBadRequest, "INVALID_MENTION", err.Error())
			return
		case errors.Is(err, service.ErrInvalidPatch):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case err != nil:
			writeObjectError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, obj)
	}
}

// DeleteObject handles DELETE /api/v1/objects/{id}: the object and its
// edges. A missing object is a 404.
func DeleteObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := svc.DeleteObject(r.Context(), id); err != nil {
			writeObjectError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// reprocessRequest is the body of POST /api/v1/objects/{id}/reprocess.
type reprocessRequest struct {
	Step string `json:"step"`
}

// ReprocessObject handles POST /api/v1/objects/{id}/reprocess: it
// enqueues a job that re-runs one enrichment step on the object with
// this instance's providers, and answers 202 with the job ID.
func ReprocessObject(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req reprocessRequest
		if !decodeStrict(w, r, &req) {
			return
		}
		job, err := svc.ReprocessObject(r.Context(), id, req.Step)
		switch {
		case errors.Is(err, jobs.ErrUnknownReprocessStep):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case err != nil:
			writeObjectError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, map[string]string{
			"job_id":    job.ID,
			"object_id": id,
			"step":      req.Step,
		})
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
