package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// CreateImport handles batch import creation.
func CreateImport(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Content string `json:"content"`
			Data    string `json:"data"`
			Format  string `json:"format"` // "jsonl", "csv", "tsv", "markdown", "opml"
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
			return
		}
		// Accept either "content" or "data" field.
		if req.Content == "" {
			req.Content = req.Data
		}
		if req.Content == "" {
			WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "content or data is required")
			return
		}
		if req.Format == "" {
			req.Format = "jsonl"
		}

		batch, err := svc.CreateBatch(r.Context(), req.Content, req.Format)
		if err != nil {
			code := http.StatusInternalServerError
			if strings.Contains(err.Error(), "exceeds limit") {
				code = http.StatusRequestEntityTooLarge
			} else if strings.Contains(err.Error(), "invalid") {
				code = http.StatusBadRequest
			}
			WriteError(w, code, "INVALID_REQUEST", err.Error())
			return
		}

		WriteJSON(w, http.StatusAccepted, batch)
	}
}

// GetImport returns a batch import by ID.
func GetImport(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		batch, err := svc.GetBatch(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "batch not found")
			return
		}
		WriteJSON(w, http.StatusOK, batch)
	}
}
