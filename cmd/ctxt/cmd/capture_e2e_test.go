package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// e2eFixture spins up an httptest.Server mocking /api/v1/analyze,
// /api/v1/inbox, and /api/v1/jobs/<id> against the binary built once by
// e2eBinary().
type e2eFixture struct {
	binary string
	server *httptest.Server
	calls  *sync.Map // path -> *[]captureRecord
}

// e2eBinary returns a path to a built `ctxt` binary, lazily compiled and
// shared across the entire e2e test run. Compiling once amortizes the
// ~4-second go build cost across every TestE2ECapture* case.
var (
	e2eBinaryPath string
	e2eBinaryErr  error
	e2eBinaryOnce sync.Once
)

func e2eBinary(t *testing.T) string {
	t.Helper()
	e2eBinaryOnce.Do(func() {
		if _, err := exec.LookPath("go"); err != nil {
			e2eBinaryErr = fmt.Errorf("go binary not on PATH")
			return
		}
		dir, err := os.MkdirTemp("", "ctxt-e2e-")
		if err != nil {
			e2eBinaryErr = err
			return
		}
		bin := filepath.Join(dir, "ctxt")
		cmd := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", bin, "./cmd/ctxt")
		cmd.Dir = mustRepoRoot(t)
		if out, err := cmd.CombinedOutput(); err != nil {
			e2eBinaryErr = fmt.Errorf("go build: %v\n%s", err, out)
			return
		}
		e2eBinaryPath = bin
	})
	if e2eBinaryErr != nil {
		t.Skipf("e2e: %v", e2eBinaryErr)
	}
	return e2eBinaryPath
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	bin := e2eBinary(t)

	calls := &sync.Map{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		rec := captureRecord{Path: r.URL.Path, Method: r.Method, Body: body}
		v, _ := calls.LoadOrStore(r.URL.Path, &[]captureRecord{})
		recs := v.(*[]captureRecord)
		*recs = append(*recs, rec)
		switch {
		case r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "e2e_job"})
		case r.URL.Path == "/api/v1/inbox" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "e2e_inbox"})
		case strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "completed"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	return &e2eFixture{binary: bin, server: srv, calls: calls}
}

func (f *e2eFixture) recordsFor(path string) []captureRecord {
	v, ok := f.calls.Load(path)
	if !ok {
		return nil
	}
	return *v.(*[]captureRecord)
}

func (f *e2eFixture) run(t *testing.T, stdin string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	full := append([]string{"capture", "--server", f.server.URL}, args...)
	cmd := exec.Command(f.binary, full...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	cmd.Env = append(os.Environ(), "CTXT_NO_CLIPBOARD=1")
	err := cmd.Run()
	exit = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			exit = -1
		}
	}
	return so.String(), se.String(), exit
}

// mustRepoRoot walks up from CWD to find go.mod (the repo root).
func mustRepoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("e2e: could not locate repo root (go.mod)")
	return ""
}

// --- Cases ---

func TestE2ECapturePositionalURL(t *testing.T) {
	f := newE2EFixture(t)
	out, errOut, exit := f.run(t, "", "https://example.com/post")
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	if !strings.Contains(out, "Job ID: e2e_job") {
		t.Errorf("expected Job ID; stdout=%q", out)
	}
	recs := f.recordsFor("/api/v1/analyze")
	if len(recs) != 1 {
		t.Fatalf("expected 1 analyze POST, got %d", len(recs))
	}
	if got := recs[0].Body["content"]; got != "https://example.com/post" {
		t.Errorf("body.content=%v", got)
	}
}

func TestE2ECapturePositionalLiteral(t *testing.T) {
	f := newE2EFixture(t)
	out, errOut, exit := f.run(t, "", "literal capture text")
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	if !strings.Contains(out, "Job ID: e2e_job") {
		t.Errorf("expected Job ID; stdout=%q", out)
	}
	recs := f.recordsFor("/api/v1/analyze")
	if recs[0].Body["source"] != "argument" {
		t.Errorf("body.source=%v want \"argument\"", recs[0].Body["source"])
	}
}

func TestE2ECapturePositionalFile(t *testing.T) {
	f := newE2EFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("hello e2e"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, errOut, exit := f.run(t, "", path)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	recs := f.recordsFor("/api/v1/analyze")
	if got := recs[0].Body["content"]; got != "hello e2e" {
		t.Errorf("body.content=%q", got)
	}
	if src, _ := recs[0].Body["source"].(string); !strings.HasPrefix(src, "file:") {
		t.Errorf("body.source=%q want file:<path>", src)
	}
}

func TestE2ECaptureStdin(t *testing.T) {
	f := newE2EFixture(t)
	out, errOut, exit := f.run(t, "stdin payload", "--stdin")
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	recs := f.recordsFor("/api/v1/analyze")
	if got := recs[0].Body["content"]; got != "stdin payload" {
		t.Errorf("body.content=%q", got)
	}
	if got := recs[0].Body["source"]; got != "stdin" {
		t.Errorf("body.source=%v", got)
	}
}

func TestE2ECaptureHintMention(t *testing.T) {
	f := newE2EFixture(t)
	_, errOut, exit := f.run(t, "", "ux idea",
		"--hint", "ux,research",
		"--mention", "@project.alpha",
	)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errOut)
	}
	recs := f.recordsFor("/api/v1/analyze")
	hs, _ := recs[0].Body["hints"].([]any)
	if len(hs) != 2 || hs[0] != "ux" || hs[1] != "research" {
		t.Errorf("body.hints=%v want [\"ux\", \"research\"]", hs)
	}
	mens, _ := recs[0].Body["mentions"].([]any)
	if len(mens) != 1 || mens[0] != "@project.alpha" {
		t.Errorf("body.mentions=%v", mens)
	}
}

func TestE2ECaptureInbox(t *testing.T) {
	f := newE2EFixture(t)
	out, errOut, exit := f.run(t, "", "park this",
		"--inbox",
		"--note", "kickoff",
	)
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	if !strings.Contains(out, "Inbox object: e2e_inbox") {
		t.Errorf("expected inbox id; stdout=%q", out)
	}
	if recs := f.recordsFor("/api/v1/inbox"); len(recs) != 1 {
		t.Fatalf("expected 1 inbox POST; got %d", len(recs))
	}
}

func TestE2ECaptureWait(t *testing.T) {
	f := newE2EFixture(t)
	out, errOut, exit := f.run(t, "", "wait test", "--wait")
	if exit != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, out, errOut)
	}
	if !strings.Contains(out, "Job ID: e2e_job") {
		t.Errorf("expected Job ID; stdout=%q", out)
	}
	// At least one /api/v1/jobs/<id> GET must have happened.
	hit := false
	f.calls.Range(func(k, _ any) bool {
		if strings.HasPrefix(k.(string), "/api/v1/jobs/") {
			hit = true
			return false
		}
		return true
	})
	if !hit {
		t.Errorf("--wait should poll /api/v1/jobs/<id>")
	}
}

func TestE2ECaptureEveryShortLoop(t *testing.T) {
	f := newE2EFixture(t)
	full := append([]string{"capture", "--server", f.server.URL}, "loop content", "--every", "100ms")
	cmd := exec.Command(f.binary, full...)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	cmd.Env = append(os.Environ(), "CTXT_NO_CLIPBOARD=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(280 * time.Millisecond)
	_ = cmd.Process.Signal(os.Interrupt)
	_ = cmd.Wait()

	count := len(f.recordsFor("/api/v1/analyze"))
	if count < 2 || count > 5 {
		t.Errorf("expected 2-5 captures in 280ms with --every 100ms, got %d", count)
	}
}

func TestE2ECaptureAmbientErrors(t *testing.T) {
	f := newE2EFixture(t)
	_, errOut, exit := f.run(t, "", "--ambient")
	if exit == 0 {
		t.Fatal("--ambient should exit non-zero")
	}
	if !strings.Contains(errOut, "ambient mode not yet implemented") {
		t.Errorf("expected ambient error message; stderr=%q", errOut)
	}
}

func TestE2ECaptureInputErrors(t *testing.T) {
	f := newE2EFixture(t)
	_, errOut, exit := f.run(t, "", "--input", "clipboard", "anything")
	if exit == 0 {
		t.Fatal("--input should exit non-zero in Track 1")
	}
	if !strings.Contains(errOut, "Track 2") {
		t.Errorf("expected Track 2 error; stderr=%q", errOut)
	}
}

func TestE2ECaptureStdinPlusPositional(t *testing.T) {
	f := newE2EFixture(t)
	_, errOut, exit := f.run(t, "x", "--stdin", "literal")
	if exit == 0 {
		t.Fatal("--stdin + positional should exit non-zero")
	}
	if !strings.Contains(errOut, "cannot combine --stdin with a positional source") {
		t.Errorf("expected mutex error; stderr=%q", errOut)
	}
}
