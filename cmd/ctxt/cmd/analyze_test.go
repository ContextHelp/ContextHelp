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
// /jobs/{id} responds with status=done so --wait tests don't block here.
func startMockDPKMS(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "job_12345678"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "job_12345678",
				"status": "done",
			})
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
		"--hint", "ux,bug",
		"--mention", "@ui.best-practice",
		"--pipeline", "url.article",
		"--language", "fr",
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

// TestAnalyzeUnsupportedTypeErrors covers T-0562: when the server rejects
// the request with 422 (no pipeline registered for the requested type),
// the CLI must exit non-zero and print the server-supplied error to stderr
// so the operator can see that the file/content was not enqueued. Previously
// this path returned a Job ID and silently dropped the work.
func TestAnalyzeUnsupportedTypeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "PIPELINE_NOT_FOUND",
				"message": `analyze: pipeline not found: type="document" pipeline="text.short" (no pipeline registered for this content type)`,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	pdf := filepath.Join(dir, "stub.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4 stub bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("analyze", "--file", pdf, "--type", "document", "--server", srv.URL)
	if err == nil {
		t.Fatalf("analyze --type document with unrouted pipeline must fail; got out=%q", out)
	}
	if !strings.Contains(err.Error(), "PIPELINE_NOT_FOUND") &&
		!strings.Contains(err.Error(), "pipeline not found") {
		t.Errorf("error must mention the unrouted-pipeline cause: %v", err)
	}
	if !strings.Contains(err.Error(), "422") {
		t.Errorf("error should expose the 422 status so operators can recognise it: %v", err)
	}
	if strings.Contains(out, "Job ID:") {
		t.Errorf("CLI must NOT print a Job ID for a rejected request; got %q", out)
	}
}

// TestAnalyzeWaitDetectsSilentDrop covers T-0562 AC2: when --wait is set
// and the job ID returned by the analyze endpoint can't be located via
// GET /jobs/{id}, the CLI must surface a clear "silently dropped" error
// instead of exiting 0 and leaving the user to guess.
func TestAnalyzeWaitDetectsSilentDrop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "ghost-job-123"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet:
			// Simulate the silent-drop class of bug: the job ID returned by
			// POST /analyze is never persisted, so GET /jobs/{id} 404s.
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("analyze", "ghost content", "--wait", "--server", srv.URL)
	if err == nil {
		t.Fatalf("analyze --wait must fail when the job ID isn't in the queue; got out=%q", out)
	}
	if !strings.Contains(err.Error(), "silently dropped") {
		t.Errorf("error must call out the silent-drop case: %v", err)
	}
	if !strings.Contains(out, "Job ID: ghost-job-123") {
		t.Errorf("CLI should still print the returned Job ID before failing: %q", out)
	}
}

// TestAnalyzeWaitSucceedsOnDone confirms --wait polls through to a terminal
// state and reports success when the job actually completes.
func TestAnalyzeWaitSucceedsOnDone(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "done-job-1"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet:
			calls++
			status := "pending"
			if calls >= 2 {
				status = "done"
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"id":     "done-job-1",
				"status": status,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("analyze", "real content", "--wait", "--server", srv.URL)
	if err != nil {
		t.Fatalf("analyze --wait should succeed on terminal status: %v", err)
	}
	if !strings.Contains(out, "Job done-job-1: done") {
		t.Errorf("output should report terminal status; got %q", out)
	}
}

func TestAnalyzeHelp(t *testing.T) {
	out, err := executeCommand("analyze", "--help")
	if err != nil {
		t.Fatalf("analyze --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--file", "--hint", "--mention", "--pipeline", "--language", "--translate", "--raw", "--wait"} {
		if !strings.Contains(out, flag) {
			t.Errorf("analyze help should list flag %s", flag)
		}
	}
}
