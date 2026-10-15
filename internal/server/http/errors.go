package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// CodeNotFound is the error code of a 404: the route, or the object a
// route names, does not exist.
const CodeNotFound = "NOT_FOUND"

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

// WriteError writes a JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string) {
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
