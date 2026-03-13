package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
)

// ListSuggestions returns objects with pending autosuggest suggestions.
func ListSuggestions(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		objs, _, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: 1000})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		type item struct {
			ObjectID string   `json:"object_id"`
			Tags     []string `json:"tags"`
			Mentions []string `json:"mentions"`
		}
		var results []item
		for _, obj := range objs {
			tags, mentions := autosuggest.GetPending(obj)
			if len(tags) > 0 || len(mentions) > 0 {
				results = append(results, item{ObjectID: obj.ID, Tags: tags, Mentions: mentions})
			}
		}
		if results == nil {
			results = []item{}
		}
		WriteJSON(w, http.StatusOK, results)
	}
}

// ApproveSuggestion applies pending suggestions to an object.
func ApproveSuggestion(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParam(r, "id")

		obj, err := svc.GetObject(ctx, id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
			return
		}

		tags, mentions := autosuggest.GetPending(obj)
		if err := autosuggest.ApplyGenerate(obj, tags, mentions); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		autosuggest.ClearPending(obj)

		if err := svc.UpdateObject(ctx, obj); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "approved", "id": id})
	}
}

// RejectSuggestion discards pending suggestions for an object.
func RejectSuggestion(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		id := chi.URLParam(r, "id")

		obj, err := svc.GetObject(ctx, id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "object not found")
			return
		}
		autosuggest.ClearPending(obj)
		if err := svc.UpdateObject(ctx, obj); err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
	}
}
