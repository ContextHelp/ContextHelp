package http

import (
	"encoding/json"
	"net/http"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// RetrieveRequest is the JSON body for a progressive retrieval request.
type RetrieveRequest struct {
	Query               string         `json:"query"`
	ConversationHistory []string       `json:"conversation_history,omitempty"`
	Filter              map[string]any `json:"filter,omitempty"`
}

// RetrieveResponse is the JSON body returned by the retrieve endpoint.
type RetrieveResponse struct {
	NeedsRetrieval bool     `json:"needs_retrieval"`
	OriginalQuery  string   `json:"original_query"`
	RewrittenQuery string   `json:"rewritten_query,omitempty"`
	NextStepQuery  string   `json:"next_step_query,omitempty"`
	Categories     []string `json:"categories,omitempty"`
	Items          []string `json:"items,omitempty"`
	Resources      []string `json:"resources,omitempty"`
}

// Retrieve is the HTTP handler for the progressive retrieval endpoint.
func Retrieve(workflow *retrieval.Workflow) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RetrieveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}

		if req.Query == "" {
			WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "query is required")
			return
		}

		filter := storage.ObjectFilter{}
		if t, ok := req.Filter["type"].(string); ok {
			filter.Type = t
		}

		result, err := workflow.Retrieve(r.Context(), req.Query, req.ConversationHistory, filter)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "RETRIEVAL_ERROR", err.Error())
			return
		}

		resp := RetrieveResponse{
			NeedsRetrieval: result.NeedsRetrieval,
			OriginalQuery:  result.OriginalQuery,
			RewrittenQuery: result.RewrittenQuery,
			NextStepQuery:  result.NextStepQuery,
		}

		for _, cat := range result.Categories {
			resp.Categories = append(resp.Categories, cat.ID)
		}
		for _, item := range result.Items {
			resp.Items = append(resp.Items, item.ID)
		}
		for _, res := range result.Resources {
			resp.Resources = append(resp.Resources, res.ID)
		}

		WriteJSON(w, http.StatusOK, resp)
	}
}
