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

const discordCLITestExport = `{
  "guild": {"id": "111111111111111111", "name": "My Server"},
  "channel": {"id": "222222222222222222", "name": "general", "type": "GuildTextChat"},
  "messages": [
    {
      "id": "333333333333333333",
      "type": "Default",
      "timestamp": "2024-07-01T08:00:00.000+00:00",
      "content": "Project update ready for review!",
      "author": {"id": "444444444444444444", "name": "alice", "isBot": false},
      "attachments": [],
      "embeds": [],
      "reactions": [{"emoji": {"name": "thumbsup"}, "count": 3}],
      "reference": {}
    },
    {
      "id": "333333333333333334",
      "type": "Default",
      "timestamp": "2024-07-01T08:01:00.000+00:00",
      "content": "Looks great!",
      "author": {"id": "555555555555555555", "name": "bob", "isBot": false},
      "attachments": [],
      "embeds": [],
      "reactions": [],
      "reference": {"messageId": "333333333333333333"}
    }
  ]
}`

func writeDiscordExport(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "export.json")
	if err := os.WriteFile(path, []byte(discordCLITestExport), 0644); err != nil {
		t.Fatalf("write export: %v", err)
	}
	return path
}

func TestImportDiscordDryRun(t *testing.T) {
	file := writeDiscordExport(t)

	out, err := executeCommand("import", "discord", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import discord dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Discord messages selected: 2 (dry-run)") {
		t.Fatalf("expected selected count, got:\n%s", out)
	}
	if !strings.Contains(out, "Scanned: 2, Skipped: 0") {
		t.Fatalf("expected scanned/skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Fatalf("expected alice in preview, got:\n%s", out)
	}
}

func TestImportDiscordDryRunSinceFilter(t *testing.T) {
	file := writeDiscordExport(t)

	// Both messages are in 2024 — since 2025-01-01 should filter them all out.
	_, err := executeCommand("import", "discord", "--file", file, "--since", "2025-01-01", "--dry-run")
	if err == nil {
		t.Fatal("expected error when since filter eliminates all messages")
	}
	if !strings.Contains(err.Error(), "no discord messages matched") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportDiscordDryRunMaxItems(t *testing.T) {
	file := writeDiscordExport(t)

	out, err := executeCommand("import", "discord", "--file", file, "--max-items", "1", "--dry-run")
	if err != nil {
		t.Fatalf("import discord max-items dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Discord messages selected: 1 (dry-run)") {
		t.Fatalf("expected 1 message with max-items=1, got:\n%s", out)
	}
}

func TestImportDiscordEnqueueSuccess(t *testing.T) {
	file := writeDiscordExport(t)

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
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_discord_1"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "discord", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import discord should succeed: %v", err)
	}

	if !strings.Contains(out, "Discord messages processed: 2") {
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
	if !strings.Contains(got[0].Source, "discord:") {
		t.Errorf("request source = %q, expected discord: prefix", got[0].Source)
	}
}

func TestImportDiscordRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "discord")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}

func TestImportDiscordInvalidSince(t *testing.T) {
	file := writeDiscordExport(t)
	_, err := executeCommand("import", "discord", "--file", file, "--since", "not-a-date", "--dry-run")
	if err == nil {
		t.Fatal("expected invalid --since to fail")
	}
	if !strings.Contains(err.Error(), "invalid --since") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportDiscordInvalidFile(t *testing.T) {
	_, err := executeCommand("import", "discord", "--file", "/nonexistent/path/export.json", "--dry-run")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestImportDiscordInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := executeCommand("import", "discord", "--file", path, "--dry-run")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
