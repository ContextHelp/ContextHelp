package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "bad input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", w.Code)
	}

	var env ErrorEnvelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error.Code != "INVALID_REQUEST" {
		t.Errorf("code: got %q", env.Error.Code)
	}
	if env.Error.Message != "bad input" {
		t.Errorf("message: got %q", env.Error.Message)
	}
	if env.Error.Details == nil {
		t.Error("details should not be nil")
	}
}

func TestNotFoundError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusNotFound, "NOT_FOUND", "not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d", w.Code)
	}

	var env ErrorEnvelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code: got %q", env.Error.Code)
	}
}

func TestBadRequestError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, http.StatusBadRequest, "INVALID_REQUEST", "validation failed")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", w.Code)
	}

	var env ErrorEnvelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error.Code != "INVALID_REQUEST" {
		t.Errorf("code: got %q", env.Error.Code)
	}
}
