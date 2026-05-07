package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCaptureInbox_PersistsProfileAndNote is the regression for T-0588:
// the /api/v1/inbox endpoint must accept "profile" + "inbox_note" JSON
// fields and persist them onto the resulting KnowledgeObject as
// ProfileID + InboxNote. Pre-T-0588, "profile" was silently dropped
// at the JSON-decode boundary because the inline request struct had
// no field for it.
func TestCaptureInbox_PersistsProfileAndNote(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := CaptureInbox(ts.svc)

	body, _ := json.Marshal(map[string]any{
		"content":    "raw inbox content",
		"type":       "text",
		"source":     "test",
		"inbox_note": "client kickoff 2026-Q2",
		"profile":    "founder",
		"hints":      []string{"research"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	id, _ := resp["id"].(string)
	if id == "" {
		t.Fatal("expected non-empty object id in response")
	}

	// Round-trip via the storage layer to confirm the values landed
	// where Service.CaptureToInbox is supposed to write them.
	obj, err := ts.svc.Store.Objects().Get(context.Background(), id)
	if err != nil {
		t.Fatalf("get persisted object: %v", err)
	}
	if obj.ProfileID != "founder" {
		t.Errorf("ProfileID = %q, want %q", obj.ProfileID, "founder")
	}
	if obj.InboxNote != "client kickoff 2026-Q2" {
		t.Errorf("InboxNote = %q, want %q", obj.InboxNote, "client kickoff 2026-Q2")
	}
	if obj.Status != "inbox" {
		t.Errorf("Status = %q, want %q", obj.Status, "inbox")
	}
}

// TestCaptureInbox_MissingContent is a sanity check that the handler
// rejects empty content with 400. Covers the InboxCaptureRequest happy
// path's pre-condition rather than the new T-0588 fields specifically.
func TestCaptureInbox_MissingContent(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := CaptureInbox(ts.svc)

	body, _ := json.Marshal(map[string]any{"type": "text"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/inbox", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
