package http

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// maxFindBody caps a find request body.
const maxFindBody = 1 << 20

// Find handles POST /api/v1/find: full-text, vector or hybrid search with
// diagnostics, and optionally a score breakdown and facet counts. sem
// embeds the query with the provider configured where dpkms runs.
//
// A malformed body is 400 INVALID_REQUEST. A semantic leg that cannot run
// while the request turns fallback_to_fts off is 422
// SEMANTIC_UNAVAILABLE, naming the status and model in details. With the
// fallback on (the default) the search answers full-text only and says
// why in diagnostics.semantic.
func Find(svc *service.Service, sem retrieval.SemanticSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.FindRequest
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFindBody))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: "+err.Error())
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body: "+err.Error())
			return
		}
		// A present but empty filter.tagged is an error, as on GET /objects
		// and in MCP; the decoded string cannot tell it from an absent key.
		var probe struct {
			Filter struct {
				Tagged *string `json:"tagged"`
			} `json:"filter"`
		}
		if json.Unmarshal(raw, &probe) == nil && probe.Filter.Tagged != nil && strings.TrimSpace(*probe.Filter.Tagged) == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "filter.tagged is empty: omit it to select every tag")
			return
		}

		req.RecordHistory = true // a person's search; the MCP tool does not set it
		res, err := svc.Find(r.Context(), req, sem)
		var unavailable *service.SemanticUnavailableError
		switch {
		case err == nil:
			WriteJSON(w, http.StatusOK, res)
		case errors.Is(err, service.ErrInvalidFind):
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		case errors.As(err, &unavailable):
			details := map[string]any{"status": unavailable.Report.Status}
			if unavailable.Report.ModelID != "" {
				details["model_id"] = unavailable.Report.ModelID
			}
			WriteJSON(w, http.StatusUnprocessableEntity, ErrorEnvelope{Error: ErrorBody{
				Code:    "SEMANTIC_UNAVAILABLE",
				Message: unavailable.Error(),
				Details: details,
			}})
		default:
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		}
	}
}
