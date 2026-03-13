package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// writeSlackExport creates a temporary Slack export directory for CLI tests.
func writeSlackExport(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	channelDir := filepath.Join(dir, "general")
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dayJSON := `[
    {
      "type": "message",
      "user": "U01ABC",
      "username": "alice",
      "text": "Project update: milestone reached!",
      "ts": "1719830400.000100"
    },
    {
      "type": "message",
      "user": "U02XYZ",
      "username": "bob",
      "text": "Great news!",
      "ts": "1719830460.000200",
      "thread_ts": "1719830400.000100"
    }
  ]`
	if err := os.WriteFile(filepath.Join(channelDir, "2024-07-01.json"), []byte(dayJSON), 0644); err != nil {
		t.Fatalf("write day file: %v", err)
	}
	return dir
}

// writeSlackExportMultiChannel creates a multi-channel Slack export for CLI tests.
func writeSlackExportMultiChannel(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	channels := map[string]string{
		"general": `[{"type":"message","user":"U01","username":"alice","text":"General msg","ts":"1719830400.000100"}]`,
		"dev":     `[{"type":"message","user":"U02","username":"bob","text":"Dev msg","ts":"1719830460.000200"}]`,
	}

	for ch, content := range channels {
		chDir := filepath.Join(dir, ch)
		if err := os.MkdirAll(chDir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", ch, err)
		}
		if err := os.WriteFile(filepath.Join(chDir, "2024-07-01.json"), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", ch, err)
		}
	}
	return dir
}

func TestImportSlackDryRun(t *testing.T) {
	dir := writeSlackExport(t)

	out, err := executeCommand("import", "slack", "--dir", dir, "--dry-run")
	if err != nil {
		t.Fatalf("import slack dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Slack messages selected: 2 (dry-run)") {
		t.Fatalf("expected selected count, got:\n%s", out)
	}
	if !strings.Contains(out, "Scanned: 2, Skipped: 0") {
		t.Fatalf("expected scanned/skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected alice in preview, got:\n%s", out)
	}
}

func TestImportSlackDryRunSinceFilter(t *testing.T) {
	dir := writeSlackExport(t)

	// 1719830400 = 2024-07-01T08:00:00Z — set since to after both messages.
	// This should return an error because all messages are filtered out.
	_, err := executeCommand("import", "slack", "--dir", dir, "--since", "2025-01-01", "--dry-run")
	if err == nil {
		t.Fatal("expected error when since filter eliminates all messages")
	}
	if !strings.Contains(err.Error(), "no slack messages matched") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportSlackDryRunChannelFilter(t *testing.T) {
	dir := writeSlackExportMultiChannel(t)

	out, err := executeCommand("import", "slack", "--dir", dir, "--channel", "general", "--dry-run")
	if err != nil {
		t.Fatalf("import slack channel filter dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Slack messages selected: 1 (dry-run)") {
		t.Fatalf("expected 1 message from #general, got:\n%s", out)
	}
	if strings.Contains(out, "Dev msg") {
		t.Fatalf("unexpected dev channel message in output:\n%s", out)
	}
}

func TestImportSlackDryRunMaxItems(t *testing.T) {
	dir := writeSlackExport(t)

	out, err := executeCommand("import", "slack", "--dir", dir, "--max-items", "1", "--dry-run")
	if err != nil {
		t.Fatalf("import slack max-items dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Slack messages selected: 1 (dry-run)") {
		t.Fatalf("expected 1 message with max-items=1, got:\n%s", out)
	}
}

func TestImportSlackEnqueueSuccess(t *testing.T) {
	dir := writeSlackExport(t)

	var (
		mu       sync.Mutex
		requests []enqueueRequest
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/enqueue" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var req enqueueRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_slack_1"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "slack", "--dir", dir, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import slack should succeed: %v", err)
	}

	if !strings.Contains(out, "Slack messages processed: 2") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected enqueue summary, got:\n%s", out)
	}

	mu.Lock()
	got := append([]enqueueRequest(nil), requests...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("expected 2 enqueue requests, got %d", len(got))
	}
	if got[0].Type != "text" {
		t.Errorf("request type = %q, want text", got[0].Type)
	}
	if !strings.Contains(got[0].Source, "slack:") {
		t.Errorf("request source = %q, expected slack: prefix", got[0].Source)
	}
}

func TestImportSlackRequiresDir(t *testing.T) {
	_, err := executeCommand("import", "slack")
	if err == nil {
		t.Fatal("expected missing --dir to fail")
	}
}

func TestImportSlackInvalidSince(t *testing.T) {
	dir := writeSlackExport(t)
	_, err := executeCommand("import", "slack", "--dir", dir, "--since", "not-a-date", "--dry-run")
	if err == nil {
		t.Fatal("expected invalid --since to fail")
	}
	if !strings.Contains(err.Error(), "invalid --since") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportSlackEmptyDir(t *testing.T) {
	dir := t.TempDir()
	// Create a channel dir with no day files.
	chDir := filepath.Join(dir, "general")
	if err := os.MkdirAll(chDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := executeCommand("import", "slack", "--dir", dir, "--dry-run")
	if err == nil {
		t.Fatal("expected error for no messages")
	}
}
