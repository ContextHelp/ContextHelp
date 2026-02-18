package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAnalyze(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"content": "some text to analyze",
		"type":    "text",
		"source":  "test",
	})

	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status: got %d, want 202", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["job_id"] == "" {
		t.Error("expected non-empty job_id")
	}
}

func TestAnalyzeMissingContent(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"type": "text",
	})

	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}

	var env ErrorEnvelope
	json.NewDecoder(resp.Body).Decode(&env)
	if env.Error.Code != "INVALID_REQUEST" {
		t.Errorf("code: got %q", env.Error.Code)
	}
}

func TestAnalyzeInvalidJSON(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader([]byte("not json")))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}
