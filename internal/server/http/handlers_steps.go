package http

import (
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/service"
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

// InstallStep handles step installation.
func InstallStep(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Name         string `json:"name"`
			FromRegistry string `json:"from_registry"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}

		if req.Name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if req.FromRegistry == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "from_registry is required")
			return
		}

		if err := svc.InstallStep(r.Context(), req.Name, req.FromRegistry); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]string{
			"status": "installed",
		})
	}
}

// UninstallStep handles step uninstallation.
func UninstallStep(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
			return
		}

		if err := svc.UninstallStep(r.Context(), name); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}

		WriteJSON(w, http.StatusNoContent, nil)
	}
}
