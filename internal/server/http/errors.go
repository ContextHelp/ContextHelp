package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// CodeNotFound is the error code of a 404: the route, or the object a
// route names, does not exist.
const CodeNotFound = "NOT_FOUND"

// CodeNotShared is the error code of a 403 on a route a restricted
// viewer's capabilities close (ADR-080 §4): the route is withheld as a
// whole, never a row, so the answer confirms nothing about any row.
const CodeNotShared = "NOT_SHARED"

// notSharedMessage is the body of every NOT_SHARED answer.
const notSharedMessage = "not shared with this principal"

// ErrorEnvelope is the standard error response.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody contains error details.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

// WriteError writes a JSON error response. A 500 that carries a read
// the viewer's scoped driver refused (storage.ErrRestrictedViewer) is
// answered as 403 NOT_SHARED instead: the read belongs to an owner-only
// store the capability table should have closed, and the refusal is
// the capability's, not a server failure. The route table closes those
// routes first; this is the backstop behind it. A refusal because no
// viewer was attached at all (storage.ErrNoViewer) is a transport bug
// and stays a 500.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	if status == http.StatusInternalServerError && strings.Contains(message, storage.ErrRestrictedViewer.Error()) &&
		!strings.Contains(message, storage.ErrNoViewer.Error()) {
		status, code, message = http.StatusForbidden, CodeNotShared, notSharedMessage
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent; an encode failure means the client
	// went away, and there is no one left to tell.
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Error: ErrorBody{
			Code:    code,
			Message: message,
			Details: map[string]any{},
		},
	})
}

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// As in WriteError: past WriteHeader, a failed encode has no recipient.
	_ = json.NewEncoder(w).Encode(v)
}

// decodeOptionalBody decodes r's JSON body into v. An empty body leaves v
// untouched and is not an error; a body that does not decode is, so a
// malformed request is refused rather than half-applied.
func decodeOptionalBody(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
