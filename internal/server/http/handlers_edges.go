package http

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CreateEdge handles POST /api/v1/edges: a typed link between two
// objects, stored with its inverse unless the type is symmetric. It
// answers 201 with the stored edges, forward first.
func CreateEdge(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.LinkRequest
		if !decodeStrict(w, r, &req) {
			return
		}
		edges, err := svc.CreateLink(r.Context(), req)
		switch {
		case errors.Is(err, service.ErrInvalidLink):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
			return
		case errors.Is(err, storage.ErrNotFound):
			WriteError(w, http.StatusNotFound, CodeNotFound, err.Error())
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusCreated, map[string]any{"data": edges})
	}
}

// ObjectEdges handles GET /api/v1/objects/{id}/edges: the object's
// object-to-object edges, or with depth a breadth-first walk of its
// links (service.LinkQuery).
func ObjectEdges(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, []string{"direction", "type", "depth"}); err != nil {
			writeParamError(w, err)
			return
		}
		depth, perr := intParam(q, "depth", 0, 1, service.MaxLinkDepth)
		if perr != nil {
			writeParamError(w, perr)
			return
		}
		dir := q.Get("direction")
		switch dir {
		case "", service.LinkOut, service.LinkIn, service.LinkBoth:
		default:
			writeParamError(w, badParam("direction", "direction must be in, out or both"))
			return
		}
		hops, err := svc.ListLinks(r.Context(), service.LinkQuery{
			ObjectID: chi.URLParam(r, "id"), Direction: dir, Type: q.Get("type"), Depth: depth,
		})
		switch {
		case errors.Is(err, storage.ErrNotFound):
			WriteError(w, http.StatusNotFound, CodeNotFound, "object not found")
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if hops == nil {
			hops = []service.LinkHop{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": hops})
	}
}

// DeleteEdges handles DELETE /api/v1/edges?source=&target=&type=: it
// removes the links between two objects in both directions, the forward
// and inverse edges together, and answers with the count.
func DeleteEdges(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if err := checkParams(q, []string{"source", "target", "type"}); err != nil {
			writeParamError(w, err)
			return
		}
		for _, name := range []string{"source", "target"} {
			if q.Get(name) == "" {
				writeParamError(w, badParam(name, "parameter %s is required", name))
				return
			}
		}
		n, err := svc.DeleteLinks(r.Context(), q.Get("source"), q.Get("target"), q.Get("type"))
		switch {
		case errors.Is(err, service.ErrInvalidLink):
			writeParamError(w, badParam("type", "%s", err.Error()))
			return
		case err != nil:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]int{"deleted": n})
	}
}
