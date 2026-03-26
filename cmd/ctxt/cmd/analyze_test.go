package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startMockDPKMS starts a mock dpkms server that accepts analyze requests.
func startMockDPKMS(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "job_12345678"})
			return
		}
		http.NotFound(w, r)
	}))
}

func TestAnalyzeWithArgument(t *testing.T) {
	srv := startMockDPKMS(t)
	defer srv.Close()

	out, err := executeCommand("analyze", "test insight", "--server", srv.URL)
	if err != nil {
		t.Fatalf("analyze with argument should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_12345678") {
		t.Error("output should contain job ID")
	}
}

func TestAnalyzeWithFile(t *testing.T) {
	srv := startMockDPKMS(t)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(f, []byte("file content here"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("analyze", "--file", f, "--server", srv.URL)
	if err != nil {
		t.Fatalf("analyze with --file should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Error("output should contain job ID")
	}
}

func TestAnalyzeWithFlags(t *testing.T) {
	srv := startMockDPKMS(t)
	defer srv.Close()

	out, err := executeCommand("analyze", "content",
		"--type", "url",
		"--hints", "#ux #bug",
		"--mentions", "@ui.best-practice",
		"--pipeline", "url.article",
		"--lang", "fr",
		"--raw",
		"--wait",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("analyze with flags should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Error("output should contain job ID")
	}
}

func TestAnalyzeRawFlagSentInRequest(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&capturedBody)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "raw-job-1"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	out, err := executeCommand("analyze", "some raw content", "--raw", "--server", srv.URL)
	if err != nil {
		t.Fatalf("analyze --raw should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Error("output should contain job ID")
	}
	if raw, ok := capturedBody["raw"].(bool); !ok || !raw {
		t.Errorf("expected raw=true in request body, got: %v", capturedBody["raw"])
	}
}

func TestAnalyzeNoInputError(t *testing.T) {
	// When no args/file and no clipboard, should error.
	_, err := executeCommand("analyze")
	if err == nil {
		t.Error("analyze with no input should fail")
	}
}

func TestAnalyzeMissingFileError(t *testing.T) {
	_, err := executeCommand("analyze", "--file", "/nonexistent/file.txt")
	if err == nil {
		t.Error("analyze with missing file should fail")
	}
}

func TestAnalyzeHelp(t *testing.T) {
	out, err := executeCommand("analyze", "--help")
	if err != nil {
		t.Fatalf("analyze --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--file", "--hints", "--mentions", "--pipeline", "--lang", "--translate", "--raw", "--wait"} {
		if !strings.Contains(out, flag) {
			t.Errorf("analyze help should list flag %s", flag)
		}
	}
}
