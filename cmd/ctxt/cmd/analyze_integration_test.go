package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnalyzeCallsDPKMS(t *testing.T) {
	var received map[string]string

	// Start a mock dpkms server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path != "/api/v1/analyze" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		json.NewDecoder(r.Body).Decode(&received)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]string{
			"job_id": "test-job-123",
		})
	}))
	defer server.Close()

	// Run the analyze command against the mock server.
	rootCmd.SetArgs([]string{"analyze", "hello world", "--server", server.URL})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Verify the request reached the server.
	if received["content"] != "hello world" {
		t.Errorf("content: got %q, want %q", received["content"], "hello world")
	}
	// "argument" is the origin when content is passed as a CLI arg.
	// (Previously this was hardcoded to "cli", which caused url_fetcher to
	// attempt GET "cli" — see T-0227.)
	if received["source"] != "argument" {
		t.Errorf("source: got %q, want %q", received["source"], "argument")
	}
}
