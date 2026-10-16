package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// topicIndexNotBuilt is the 404 message of a topic-index read before the
// first rebuild.
const topicIndexNotBuilt = "topic index not built; rebuild it with POST /api/v1/topic-index"

// GetTopicIndex handles GET /api/v1/topic-index: the stored topic index,
// or 404 before the first rebuild.
func GetTopicIndex(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idx, err := svc.GetTopicIndex(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if idx == nil {
			WriteError(w, http.StatusNotFound, CodeNotFound, topicIndexNotBuilt)
			return
		}
		WriteJSON(w, http.StatusOK, idx)
	}
}

// GetTopicEntries handles GET /api/v1/topic-index/{topic}: the entries
// of the first category whose name equals or starts with topic, ignoring
// case; {"data": []} when none does, 404 before the first rebuild.
func GetTopicEntries(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idx, err := svc.GetTopicIndex(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if idx == nil {
			WriteError(w, http.StatusNotFound, CodeNotFound, topicIndexNotBuilt)
			return
		}
		entries := idx.Entries(chi.URLParam(r, "topic"))
		if entries == nil {
			entries = []service.TopicEntry{}
		}
		WriteJSON(w, http.StatusOK, map[string]any{"data": entries})
	}
}

// RebuildTopicIndex handles POST /api/v1/topic-index: rebuild the index
// from every object and return the index object's ID.
func RebuildTopicIndex(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := svc.BuildTopicIndex(r.Context())
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"id": id})
	}
}
