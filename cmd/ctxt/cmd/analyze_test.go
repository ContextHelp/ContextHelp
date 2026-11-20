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
// /health answers 200 like the real daemon — the CLI probes it to route.
func startMockDPKMS(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
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
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
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

// TestAnalyzeUnsupportedTypeErrors covers the case where the server rejects
// the request with 422 (no pipeline registered for the requested type),
// the CLI must exit non-zero and print the server-supplied error to stderr
// so the operator can see that the file/content was not enqueued. Previously
// this path returned a Job ID and silently dropped the work.
func TestAnalyzeUnsupportedTypeErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
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

// TestAnalyzeWaitDetectsSilentDrop covers the case where --wait is set
// and the job ID returned by the analyze endpoint can't be located via
// GET /jobs/{id}, the CLI must surface a clear "silently dropped" error
// instead of exiting 0 and leaving the user to guess.
func TestAnalyzeWaitDetectsSilentDrop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
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
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
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

// analyze attributes what it submits to the active profile: the explicit
// --profile, else profile.default, rides in the request body.
func TestAnalyzeStampsProfile(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			body = nil
			json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "job-p"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	if _, err := executeCommand("analyze", "some content", "--profile", "growth", "--server", srv.URL); err != nil {
		t.Fatalf("analyze --profile: %v", err)
	}
	if body["profile"] != "growth" {
		t.Errorf("profile = %v, want growth", body["profile"])
	}
	if _, err := executeCommand("analyze", "some content", "--server", srv.URL); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	if _, ok := body["profile"]; ok {
		t.Errorf("profile = %v with none active, want none", body["profile"])
	}
}

// analyzeBody runs `ctxt analyze` against a mock dpkms and returns the JSON
// request body it posted.
func analyzeBody(t *testing.T, args ...string) (map[string]any, error) {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "job-x"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	_, err := executeCommand(append([]string{"analyze"}, append(args, "--server", srv.URL)...)...)
	return body, err
}

func TestAnalyzeMentionRepeatableAndCompat(t *testing.T) {
	cases := map[string][]string{
		"repeated":   {"--mention", "@channel.wild", "--mention", "@creator.ada", "--mention", "@guest.bob"},
		"csv":        {"--mention", "@channel.wild,@creator.ada,@guest.bob"},
		"whitespace": {"--mention", "@channel.wild @creator.ada @guest.bob"},
		"mixed":      {"--mention", "@channel.wild,@creator.ada", "--mention", "@guest.bob"},
	}
	for name, flags := range cases {
		t.Run(name, func(t *testing.T) {
			body, err := analyzeBody(t, append([]string{"some text"}, flags...)...)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := body["mentions"].([]any)
			want := []any{"@channel.wild", "@creator.ada", "@guest.bob"}
			if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
				t.Errorf("mentions: got %v, want %v", got, want)
			}
		})
	}
}

func TestAnalyzeNoteDataSourceKeySentInRequest(t *testing.T) {
	body, err := analyzeBody(t, "https://youtu.be/abc", "--type", "url",
		"--note", "the full transcript",
		"--data", `{"title":"T","meta":{"views":3}}`,
		"--source-key", "youtube:abc")
	if err != nil {
		t.Fatal(err)
	}
	if body["note"] != "the full transcript" {
		t.Errorf("note: %v", body["note"])
	}
	if body["source_key"] != "youtube:abc" {
		t.Errorf("source_key: %v", body["source_key"])
	}
	data, _ := body["data"].(map[string]any)
	if data["title"] != "T" || data["meta"].(map[string]any)["views"] != float64(3) {
		t.Errorf("data: %v", body["data"])
	}
}

func TestAnalyzeNoteAndDataFromFile(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "t.txt")
	data := filepath.Join(dir, "d.json")
	os.WriteFile(note, []byte("transcript from file\n"), 0o644)
	os.WriteFile(data, []byte(`{"channel":"wild"}`), 0o644)

	body, err := analyzeBody(t, "text", "--note", "@"+note, "--data", "@"+data)
	if err != nil {
		t.Fatal(err)
	}
	if body["note"] != "transcript from file\n" {
		t.Errorf("note: %q", body["note"])
	}
	if d, _ := body["data"].(map[string]any); d["channel"] != "wild" {
		t.Errorf("data: %v", body["data"])
	}

	body, err = analyzeBody(t, "text", "--note", "@@handle is literal")
	if err != nil {
		t.Fatal(err)
	}
	if body["note"] != "@handle is literal" {
		t.Errorf("@@ must give a literal @: %q", body["note"])
	}
}

func TestAnalyzeDataRejectsBadInput(t *testing.T) {
	big := `{"x":"` + strings.Repeat("d", 300<<10) + `"}`
	for name, data := range map[string]string{
		"invalid json": `{"title":`,
		"array":        `[1,2]`,
		"scalar":       `"x"`,
		"too large":    big,
		"missing file": "@/nonexistent/d.json",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := analyzeBody(t, "text", "--data", data)
			if err == nil {
				t.Fatal("must fail before anything is sent")
			}
			if !strings.Contains(err.Error(), "--data") {
				t.Errorf("error should name the flag: %v", err)
			}
		})
	}
}

func TestAnalyzeHelpListsNoteData(t *testing.T) {
	out, err := executeCommand("analyze", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--note", "--data", "--source-key", "--no-dedup"} {
		if !strings.Contains(out, flag) {
			t.Errorf("analyze help should list %s", flag)
		}
	}
}

// --profile pins the capture to a focus profile: the request carries it,
// so a source key is unique per profile instead of global.
func TestAnalyzeProfileSentInRequest(t *testing.T) {
	body, err := analyzeBody(t, "https://youtu.be/abc", "--type", "url",
		"--source-key", "youtube:abc", "--profile", "research")
	if err != nil {
		t.Fatal(err)
	}
	if body["profile"] != "research" {
		t.Errorf("profile: %v", body["profile"])
	}
	body, err = analyzeBody(t, "https://youtu.be/abc", "--type", "url")
	if err != nil {
		t.Fatal(err)
	}
	if _, set := body["profile"]; set {
		t.Errorf("profile sent without --profile: %v", body["profile"])
	}
}
