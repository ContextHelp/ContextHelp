package http

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// aliasOperator is the minimal interface the handlers need from the aliasing plugin.
// Avoids importing the plugin module directly from the HTTP package.
type aliasOperator interface {
	SetAliasOp(ctx context.Context, alias, objectID, scope, profile string) error
	ListAliases(ctx context.Context, objectID string) ([]*storage.Alias, error)
	RemoveAlias(ctx context.Context, alias, scope, profile string) error
	ResolveID(ctx context.Context, idOrAlias, profile string) (string, error)
}

// getAliasOperator retrieves the aliasing plugin from the service's plugin registry.
func getAliasOperator(svc *service.Service) aliasOperator {
	if svc.PluginRegistry == nil {
		return nil
	}
	for _, ar := range svc.PluginRegistry.AliasResolvers() {
		if ap, ok := ar.(aliasOperator); ok {
			return ap
		}
	}
	return nil
}

// CreateAlias handles POST /api/v1/aliases
func CreateAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
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
		if req.Alias == "" || req.ObjectID == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "alias and object_id are required")
			return
		}
		if req.Scope == "" {
			req.Scope = "global"
		}
		ap := getAliasOperator(svc)
		if ap != nil {
			if err := ap.SetAliasOp(ctx, req.Alias, req.ObjectID, req.Scope, req.Profile); err != nil {
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
		}
		WriteJSON(w, http.StatusCreated, map[string]string{"alias": req.Alias, "object_id": req.ObjectID})
	}
}

// ListAliases handles GET /api/v1/aliases
func ListAliases(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		objectID := r.URL.Query().Get("object_id")
		ap := getAliasOperator(svc)
		if ap != nil {
			aliases, err := ap.ListAliases(ctx, objectID)
			if err != nil {
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			if aliases == nil {
				aliases = []*storage.Alias{}
			}
			WriteJSON(w, http.StatusOK, aliases)
			return
		}
		WriteJSON(w, http.StatusOK, []*storage.Alias{})
	}
}

// ResolveAlias handles GET /api/v1/aliases/{alias}
func ResolveAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		alias := chi.URLParam(r, "alias")
		profile := r.URL.Query().Get("profile")
		ap := getAliasOperator(svc)
		if ap != nil {
			id, err := ap.ResolveID(ctx, alias, profile)
			if err != nil || id == alias {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "alias not found")
				return
			}
			WriteJSON(w, http.StatusOK, map[string]string{"alias": alias, "object_id": id})
			return
		}
		WriteError(w, http.StatusNotFound, "NOT_FOUND", "aliasing plugin not configured")
	}
}

// DeleteAlias handles DELETE /api/v1/aliases/{alias}
func DeleteAlias(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		alias := chi.URLParam(r, "alias")
		scope := r.URL.Query().Get("scope")
		profile := r.URL.Query().Get("profile")
		if scope == "" {
			scope = "global"
		}
		ap := getAliasOperator(svc)
		if ap != nil {
			if err := ap.RemoveAlias(ctx, alias, scope, profile); err != nil {
				WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
