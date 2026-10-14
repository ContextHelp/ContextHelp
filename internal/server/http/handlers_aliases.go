package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// writeAliasError maps an alias service error to its response: 400 for a
// bad scope/profile pair, 404 for a missing alias or target object, 409 for
// a taken key, 501 for a backend without an alias store, 500 otherwise.
func writeAliasError(w http.ResponseWriter, err error, notFound string) {
	switch {
	case errors.Is(err, service.ErrInvalidAlias):
		WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
	case errors.Is(err, service.ErrAliasExists):
		WriteError(w, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, storage.ErrNotImplemented):
		WriteError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED",
			"aliases are not supported by this storage backend")
	case errors.Is(err, storage.ErrNotFound):
		WriteError(w, http.StatusNotFound, "NOT_FOUND", notFound)
	default:
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
	}
}

// CreateAlias handles POST /api/v1/aliases: stores alias → object_id and
// answers 201 with the stored record.
func CreateAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Alias    string `json:"alias"`
			ObjectID string `json:"object_id"`
			Scope    string `json:"scope"`
			Profile  string `json:"profile"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON")
			return
		}
		a, err := svc.CreateAlias(r.Context(), req.Alias, req.ObjectID, req.Scope, req.Profile)
		if err != nil {
			writeAliasError(w, err, "object not found")
			return
		}
		WriteJSON(w, http.StatusCreated, a)
	}
}

// ListAliases handles GET /api/v1/aliases
func ListAliases(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aliases, err := svc.ListAliases(r.Context(), r.URL.Query().Get("object_id"))
		if err != nil {
			writeAliasError(w, err, "alias not found")
			return
		}
		if aliases == nil {
			aliases = []*storage.Alias{}
		}
		WriteJSON(w, http.StatusOK, aliases)
	}
}

// ResolveAlias handles GET /api/v1/aliases/{alias}
func ResolveAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alias := chi.URLParam(r, "alias")
		id, err := svc.ResolveAlias(r.Context(), alias, r.URL.Query().Get("profile"))
		if err != nil {
			writeAliasError(w, err, "alias not found")
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"alias": alias, "object_id": id})
	}
}

// DeleteAlias handles DELETE /api/v1/aliases/{alias}
func DeleteAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := svc.DeleteAlias(r.Context(), chi.URLParam(r, "alias"), q.Get("scope"), q.Get("profile")); err != nil {
			writeAliasError(w, err, "alias not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
