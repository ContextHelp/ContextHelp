package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
)

// ListSteps handles step listing.
func ListSteps(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")

		steps, total, err := svc.ListSteps(r.Context(), source)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"steps": steps,
			"total": total,
		})
	}
}

// GetStep handles step retrieval.
func GetStep(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		step, err := svc.GetStep(r.Context(), name)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "step not found")
			return
		}

		WriteJSON(w, http.StatusOK, step)
	}
}

// maxStepInstallBody bounds an install request: a base64 step archive
// of steps.MaxArchiveBytes plus the JSON around it.
const maxStepInstallBody = steps.MaxArchiveBytes/3*4 + 64<<10

// InstallStep handles step installation: from a configured registry
// (from_registry) or from a packaged directory (archive, base64).
func InstallStep(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.StepInstallRequest
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStepInstallBody))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: "+err.Error())
			return
		}
		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}
		if req.FromRegistry == "" && len(req.Archive) == 0 {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "from_registry or archive is required")
			return
		}

		step, err := svc.InstallStep(r.Context(), req)
		if err != nil {
			status, code := stepErrorStatus(err)
			WriteError(w, status, code, err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{
			"status": "installed",
			"step":   step,
		})
	}
}

// UninstallStep handles step uninstallation: the step's files and its
// record go.
func UninstallStep(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if err := svc.UninstallStep(r.Context(), name); err != nil {
			status, code := stepErrorStatus(err)
			WriteError(w, status, code, err.Error())
			return
		}

		WriteJSON(w, http.StatusNoContent, nil)
	}
}

// stepErrorStatus maps install and uninstall failures to responses.
func stepErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, steps.ErrInvalidStep):
		return http.StatusBadRequest, "INVALID_STEP"
	case errors.Is(err, steps.ErrStepExists):
		return http.StatusConflict, "STEP_EXISTS"
	case errors.Is(err, service.ErrStepInUse):
		return http.StatusConflict, "STEP_IN_USE"
	case errors.Is(err, steps.ErrStepNotFound), errors.Is(err, steps.ErrNotInRegistry):
		return http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, steps.ErrUntrustedRegistry):
		return http.StatusUnprocessableEntity, "UNTRUSTED_REGISTRY"
	case errors.Is(err, steps.ErrDigestMismatch):
		return http.StatusUnprocessableEntity, "DIGEST_MISMATCH"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR"
	}
}
