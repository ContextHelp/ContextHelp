package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCapturePage_MissingURL(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := CapturePage(ts.svc)

	body, _ := json.Marshal(map[string]string{"title": "Test Page"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/page", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestCapturePage_ValidRequest(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := CapturePage(ts.svc)

	body, _ := json.Marshal(map[string]string{
		"url":     "https://example.com/article",
		"title":   "Example Article",
		"content": "The article body text.",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/page", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(rr.Body).Decode(&resp)
	if resp["job_id"] == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestCaptureSelection_MissingSelection(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := CaptureSelection(ts.svc)

	body, _ := json.Marshal(map[string]string{"url": "https://example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/capture/selection", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestListRecentCaptures(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	handler := ListRecentCaptures(ts.svc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/capture/recent", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
