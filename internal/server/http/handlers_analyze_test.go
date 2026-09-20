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

// TestAnalyzeIdempotencyKeyReplay: a replayed POST carrying the same
// idempotency_key (response lost after the first enqueue) resolves to the
// existing job — same job_id, still a success status, no duplicate enqueue.
func TestAnalyzeIdempotencyKeyReplay(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"content":         "replayed submission",
		"type":            "text",
		"source":          "test",
		"idempotency_key": "idem-http-1",
	})

	post := func() (int, string) {
		t.Helper()
		resp, err := http.Post(ts.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		var result map[string]string
		_ = json.NewDecoder(resp.Body).Decode(&result)
		return resp.StatusCode, result["job_id"]
	}

	status1, id1 := post()
	if status1 != http.StatusAccepted {
		t.Fatalf("first status: got %d, want 202", status1)
	}
	if id1 == "" {
		t.Fatal("first job_id empty")
	}

	status2, id2 := post()
	if status2 != http.StatusAccepted && status2 != http.StatusOK {
		t.Errorf("replay status: got %d, want 200/202", status2)
	}
	if id2 != id1 {
		t.Errorf("replay job_id = %q, want the original %q", id2, id1)
	}
}
