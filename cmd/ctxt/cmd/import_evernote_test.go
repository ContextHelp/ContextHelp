package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const evernoteENEXFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-export SYSTEM "http://xml.evernote.com/pub/evernote-export4.dtd">
<en-export export-date="20260218T010203Z" application="Evernote" version="10.x">
  <note>
    <guid>note-guid-1</guid>
    <title>Project Notes</title>
    <content><![CDATA[<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE en-note SYSTEM "http://xml.evernote.com/pub/enml2.dtd">
<en-note><div>Hello <b>team</b>.</div></en-note>]]></content>
    <created>20260110T090000Z</created>
    <updated>20260115T100000Z</updated>
    <tag>Work</tag>
    <note-attributes>
      <source-url>https://example.com/project</source-url>
    </note-attributes>
  </note>
  <note>
    <title>Personal List</title>
    <content><![CDATA[<en-note><div>Buy milk</div></en-note>]]></content>
    <created>20251201T080000Z</created>
    <updated>20251205T080000Z</updated>
    <tag>Personal</tag>
  </note>
</en-export>`

func TestImportEvernoteDryRunScoped(t *testing.T) {
	file := writeTempBookmarks(t, evernoteENEXFixture)

	out, err := executeCommand(
		"import", "evernote",
		"--file", file,
		"--since", "2026-01-01",
		"--tagged", "work",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("import evernote dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Evernote notes selected: 1 (dry-run)") {
		t.Fatalf("expected selected count, got:\n%s", out)
	}
	if !strings.Contains(out, "Scanned: 2, Skipped: 1") {
		t.Fatalf("expected scanned/skipped summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Project Notes") {
		t.Fatalf("expected selected note preview, got:\n%s", out)
	}
}

func TestImportEvernoteEnqueueSuccess(t *testing.T) {
	file := writeTempBookmarks(t, evernoteENEXFixture)

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
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_evernote_1"})
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "evernote",
		"--file", file,
		"--server", srv.URL,
	)
	if err != nil {
		t.Fatalf("import evernote should succeed: %v", err)
	}

	if !strings.Contains(out, "Evernote notes processed: 2") {
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
		t.Fatalf("first request type = %q", got[0].Type)
	}
	if got[0].Source != "https://example.com/project" {
		t.Fatalf("first request source = %q", got[0].Source)
	}
	if got[1].Source != "import:evernote" {
		t.Fatalf("second request source = %q", got[1].Source)
	}
}

func TestImportEvernoteRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "evernote")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}

func TestImportEvernoteInvalidSince(t *testing.T) {
	file := writeTempBookmarks(t, evernoteENEXFixture)
	_, err := executeCommand("import", "evernote", "--file", file, "--since", "bad-value", "--dry-run")
	if err == nil {
		t.Fatal("expected invalid --since to fail")
	}
	if !strings.Contains(err.Error(), "invalid --since") {
		t.Fatalf("unexpected error: %v", err)
	}
}
