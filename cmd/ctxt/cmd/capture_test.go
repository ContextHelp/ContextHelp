package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// startMockCaptureDPKMS captures the body of every POST to /api/v1/analyze
// and replies with a fixed Job ID. Tests can assert on what the CLI sent.
type captureRecord struct {
	Path        string
	Method      string
	ContentType string
	Body        map[string]any
}

func startMockCaptureDPKMS(t *testing.T, recs *[]captureRecord) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*recs = append(*recs, captureRecord{
			Path:        r.URL.Path,
			Method:      r.Method,
			ContentType: r.Header.Get("Content-Type"),
			Body:        body,
		})
		switch {
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_capture_1"})
		case r.URL.Path == "/api/v1/inbox" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "obj_inbox_1"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed"})
		default:
			http.NotFound(w, r)
		}
	}))
}

// --- Mutex / scaffolding tests ---

func TestCaptureAmbientErrors(t *testing.T) {
	_, err := executeCommand("capture", "--ambient")
	if err == nil {
		t.Fatal("--ambient should error in Track 1")
	}
	if !strings.Contains(err.Error(), "ambient mode not yet implemented") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCaptureTrack2FlagsError(t *testing.T) {
	for _, flag := range []string{"--input", "--skip"} {
		_, err := executeCommand("capture", flag, "x", "some-source")
		if err == nil {
			t.Fatalf("%s should error in Track 1", flag)
		}
		if !strings.Contains(err.Error(), "ambient mode (Track 2") {
			t.Errorf("%s: unexpected message: %v", flag, err)
		}
	}
	// --window is a duration flag; pass a duration value.
	_, err := executeCommand("capture", "--window", "5m", "some-source")
	if err == nil || !strings.Contains(err.Error(), "ambient mode (Track 2") {
		t.Errorf("--window should error in Track 1; got %v", err)
	}
}

func TestCaptureStdinPlusPositionalErrors(t *testing.T) {
	_, err := executeCommand("capture", "--stdin", "literal text")
	if err == nil || !strings.Contains(err.Error(), "cannot combine --stdin with a positional source") {
		t.Errorf("expected --stdin + positional mutex error; got %v", err)
	}
}

// --- Behavior tests ---

func TestCapturePositionalURL(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	out, err := executeCommand("capture", "https://example.com/post", "--server", srv.URL)
	if err != nil {
		t.Fatalf("capture URL: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_capture_1") {
		t.Errorf("expected Job ID in stdout, got %q", out)
	}
	if len(recs) != 1 || recs[0].Path != "/api/v1/analyze" {
		t.Fatalf("expected one analyze POST, got %+v", recs)
	}
	if got := recs[0].Body["content"]; got != "https://example.com/post" {
		t.Errorf("body.content = %v, want URL", got)
	}
	if got := recs[0].Body["source"]; got != "https://example.com/post" {
		t.Errorf("body.source = %v, want URL (analyze auto-detect path)", got)
	}
}

func TestCapturePositionalLiteral(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	_, err := executeCommand("capture", "some literal thought", "--server", srv.URL)
	if err != nil {
		t.Fatalf("capture literal: %v", err)
	}
	if got := recs[0].Body["content"]; got != "some literal thought" {
		t.Errorf("body.content = %v", got)
	}
	if got := recs[0].Body["source"]; got != "argument" {
		t.Errorf("body.source = %v, want \"argument\"", got)
	}
}

func TestCapturePositionalFile(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	dir := t.TempDir()
	f := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(f, []byte("# heading\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand("capture", f, "--server", srv.URL)
	if err != nil {
		t.Fatalf("capture file: %v", err)
	}
	if got := recs[0].Body["content"]; got != "# heading\nbody\n" {
		t.Errorf("body.content = %q, want file contents", got)
	}
	// PR #31 review fix: capture sends the absolute file path as `source`
	// (no "file:" prefix) so server-side prefix-based pipeline detectors
	// (e.g. /vault/notes/... → watch.file, T-0209) fire correctly.
	if src, _ := recs[0].Body["source"].(string); !filepath.IsAbs(src) || !strings.HasSuffix(src, filepath.Base(f)) {
		t.Errorf("body.source = %q, want absolute path ending in %q", src, filepath.Base(f))
	}
}

// TestCaptureSourceFlagOverridesAutoDetected is the regression for PR #31
// review item #2: --source <name> on the CLI was declared but never read.
// It must overwrite the auto-detected source string in the request body
// so operators can pin pipeline detection (e.g. --source /vault/notes/x.md
// when content arrives via --stdin).
func TestCaptureSourceFlagOverridesAutoDetected(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	_, err := executeCommand("capture", "literal text",
		"--source", "/vault/notes/manual-pin.md",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got, _ := recs[0].Body["source"].(string); got != "/vault/notes/manual-pin.md" {
		t.Errorf("body.source = %q, want /vault/notes/manual-pin.md (--source override)", got)
	}
}

// TestCaptureProfileAndNoteFlags is the regression for T-0588: --profile
// and --note are real flags that flow into the request body and reach
// AnalyzeRequest.Profile / AnalyzeRequest.Note server-side. Pre-T-0588
// they were declared on capture.go but never read — the values silently
// vanished before the POST.
func TestCaptureProfileAndNoteFlags(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	_, err := executeCommand("capture", "literal text",
		"--profile", "founder",
		"--note", "client kickoff 2026-Q2",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got, _ := recs[0].Body["profile"].(string); got != "founder" {
		t.Errorf("body.profile = %q, want %q", got, "founder")
	}
	if got, _ := recs[0].Body["note"].(string); got != "client kickoff 2026-Q2" {
		t.Errorf("body.note = %q, want %q", got, "client kickoff 2026-Q2")
	}
}

func TestCaptureHintAndMentionFlags(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	_, err := executeCommand("capture", "ux refresh idea",
		"--hint", "ux,research",
		"--mention", "@project.alpha",
		"--mention", "@person.bob",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	hs, _ := recs[0].Body["hints"].([]any)
	if len(hs) != 2 || hs[0] != "ux" || hs[1] != "research" {
		t.Errorf("body.hints = %v, want [\"ux\", \"research\"]", hs)
	}
	mens, _ := recs[0].Body["mentions"].([]any)
	if len(mens) != 2 {
		t.Fatalf("body.mentions = %v, want 2 entries", mens)
	}
	if mens[0] != "@project.alpha" || mens[1] != "@person.bob" {
		t.Errorf("body.mentions = %v", mens)
	}
}

func TestCaptureWaitPolls(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	out, err := executeCommand("capture", "wait test", "--wait", "--server", srv.URL)
	if err != nil {
		t.Fatalf("capture --wait: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_capture_1") {
		t.Errorf("expected Job ID in stdout, got %q", out)
	}
	// Verify we hit /api/v1/jobs/<id> at least once.
	hitJobs := false
	for _, r := range recs {
		if strings.HasPrefix(r.Path, "/api/v1/jobs/") {
			hitJobs = true
			break
		}
	}
	if !hitJobs {
		t.Errorf("--wait should poll /api/v1/jobs/<id>; recs=%+v", recs)
	}
}

func TestCaptureInboxRoutesToInboxEndpoint(t *testing.T) {
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	out, err := executeCommand("capture", "park this",
		"--inbox",
		"--note", "kickoff",
		"--hint", "research",
		"--mention", "@project.alpha",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("capture --inbox: %v", err)
	}
	if !strings.Contains(out, "Inbox object: obj_inbox_1") {
		t.Errorf("expected Inbox object id in stdout, got %q", out)
	}
	if recs[0].Path != "/api/v1/inbox" {
		t.Errorf("expected POST /api/v1/inbox, got %s", recs[0].Path)
	}
	if note, _ := recs[0].Body["inbox_note"].(string); note != "kickoff" {
		t.Errorf("body.inbox_note = %q, want \"kickoff\"", note)
	}
	hs, _ := recs[0].Body["hints"].([]any)
	if len(hs) != 1 || hs[0] != "research" {
		t.Errorf("body.hints = %v, want [\"research\"]", hs)
	}
}

func TestCaptureInboxIgnoresWait(t *testing.T) {
	// --inbox routes to /api/v1/inbox which does NOT enqueue a job; --wait
	// must NOT crash and must NOT poll /api/v1/jobs/<id>.
	var recs []captureRecord
	srv := startMockCaptureDPKMS(t, &recs)
	defer srv.Close()

	out, err := executeCommand("capture", "stash this",
		"--inbox", "--wait",
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("capture --inbox --wait: %v", err)
	}
	if !strings.Contains(out, "Inbox object: obj_inbox_1") {
		t.Errorf("expected Inbox object id in stdout, got %q", out)
	}
	for _, r := range recs {
		if strings.HasPrefix(r.Path, "/api/v1/jobs/") {
			t.Errorf("--inbox --wait should not poll /api/v1/jobs/<id>; got %s", r.Path)
		}
	}
}

func TestCaptureEveryLoops(t *testing.T) {
	var (
		mu    sync.Mutex
		count int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			mu.Lock()
			count++
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "loop_job"})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Drive RunCapture directly so we can cancel via context — executeCommand's
	// cobra harness binds context.Background by default. Bypassing Execute
	// also skips initConfig, so supply the empty config endpoint resolution
	// reads.
	prevCfg := cfg
	t.Cleanup(func() { cfg = prevCfg })
	cfg = &config.Config{}
	resetAllFlags(rootCmd)
	captureCmd.Flags().Set("server", srv.URL)
	captureCmd.Flags().Set("every", "100ms")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	captureCmd.SetContext(ctx)

	done := make(chan error, 1)
	go func() {
		done <- RunCapture(captureCmd, []string{"loop content"})
	}()

	// Expect: 1 immediate capture + ticks at 100ms, 200ms = 3 by ~250ms.
	time.Sleep(250 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("capture loop returned: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("capture loop did not return after cancel")
	}

	mu.Lock()
	got := count
	mu.Unlock()
	if got < 2 || got > 4 {
		t.Errorf("expected 2-4 captures in 250ms with --every 100ms, got %d", got)
	}
}

func TestCaptureHelpListsFlags(t *testing.T) {
	out, err := executeCommand("capture", "--help")
	if err != nil {
		t.Fatalf("capture --help: %v", err)
	}
	for _, flag := range []string{
		"--source", "--stdin", "--type",
		"--pipeline", "--inbox",
		"--every",
		"--hint", "--mention", "--note", "--profile",
		"--raw", "--no-fanout", "--no-dedup", "--source-key",
		"--wait", "--server",
		"--ambient", "--input", "--skip", "--window",
	} {
		if !strings.Contains(out, flag) {
			t.Errorf("capture help should list %s", flag)
		}
	}
}
