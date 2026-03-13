package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestImportDropboxRequiresAccessToken(t *testing.T) {
	_, err := executeCommand("import", "dropbox", "--dry-run")
	if err == nil {
		t.Fatal("expected missing token to fail")
	}
	if !strings.Contains(err.Error(), "dropbox token is required") {
		t.Fatalf("expected token guidance, got: %v", err)
	}
}

func TestImportDropboxDryRunBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/files/list_folder":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"entries": [
					{".tag":"file","id":"id:f1","name":"notes.md","path_display":"/notes.md","size":100,"server_modified":"2026-02-01T10:00:00Z","content_hash":"hash1"},
					{".tag":"file","id":"id:f2","name":"report.pdf","path_display":"/report.pdf","size":2048,"server_modified":"2026-02-10T10:00:00Z","content_hash":"hash2"},
					{".tag":"folder","id":"id:d1","name":"archive","path_display":"/archive"}
				],
				"cursor": "cursor-abc",
				"has_more": false
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "dropbox",
		"--access-token", "test-token",
		"--dropbox-api-url", srv.URL,
		"--dropbox-content-url", srv.URL,
		"--path", "/",
		"--since", "2026-01-01",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("import dropbox dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Dropbox scope:") {
		t.Fatalf("expected scope output, got:\n%s", out)
	}
	if !strings.Contains(out, "Dropbox files matched: 3 (dry-run)") {
		t.Fatalf("expected dry-run count, got:\n%s", out)
	}
	if !strings.Contains(out, "since=2026-01-01T00:00:00Z") {
		t.Fatalf("expected since scope value, got:\n%s", out)
	}
	if !strings.Contains(out, "cursor-abc") {
		t.Fatalf("expected cursor in output, got:\n%s", out)
	}
}

func TestImportDropboxNoFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"entries":[],"cursor":"c1","has_more":false}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "dropbox",
		"--access-token", "test-token",
		"--dropbox-api-url", srv.URL,
		"--dropbox-content-url", srv.URL,
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("no-file dry-run should succeed: %v", err)
	}
	if !strings.Contains(out, "No Dropbox files matched") {
		t.Fatalf("expected no-files message, got:\n%s", out)
	}
}

func TestImportDropboxEnqueueSuccess(t *testing.T) {
	dropboxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/2/files/list_folder" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"entries": [
					{".tag":"file","id":"id:f1","name":"notes.md","path_display":"/notes.md","size":100,"server_modified":"2026-02-01T10:00:00Z","content_hash":"hash1"},
					{".tag":"file","id":"id:f2","name":"photo.jpg","path_display":"/photo.jpg","size":50000,"server_modified":"2026-02-01T10:00:00Z","content_hash":"hash2"},
					{".tag":"folder","id":"id:d1","name":"bin","path_display":"/bin"}
				],
				"cursor": "cursor-new",
				"has_more": false
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer dropboxSrv.Close()

	var (
		mu         sync.Mutex
		enqueued   int
		sourceOK   int
	)

	dpkmsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		enqueued++
		if req.Source == "import:dropbox" {
			sourceOK++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"job_id":"job_dbx_1"}`))
	}))
	defer dpkmsSrv.Close()

	out, err := executeCommand(
		"import", "dropbox",
		"--access-token", "tok",
		"--dropbox-api-url", dropboxSrv.URL,
		"--dropbox-content-url", dropboxSrv.URL,
		"--server", dpkmsSrv.URL,
	)
	if err != nil {
		t.Fatalf("import dropbox should succeed: %v", err)
	}

	if !strings.Contains(out, "Dropbox files scanned: 3") {
		t.Fatalf("expected scanned summary, got:\n%s", out)
	}
	// Only notes.md gets imported (supported ext); photo.jpg is skipped; bin is skipped.
	if !strings.Contains(out, "Imported: 1") {
		t.Fatalf("expected imported=1, got:\n%s", out)
	}
	if !strings.Contains(out, "Skipped: 2") {
		t.Fatalf("expected skipped=2, got:\n%s", out)
	}

	mu.Lock()
	gotEnqueued := enqueued
	gotSource := sourceOK
	mu.Unlock()

	if gotEnqueued != 1 {
		t.Errorf("expected 1 enqueued, got %d", gotEnqueued)
	}
	if gotSource != 1 {
		t.Errorf("expected source=import:dropbox on enqueue, got %d", gotSource)
	}
}

func TestImportDropboxCursorResume(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/files/list_folder/continue":
			var req map[string]string
			json.NewDecoder(r.Body).Decode(&req)
			if req["cursor"] != "saved-cursor" {
				http.Error(w, "unexpected cursor", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"entries": [
					{".tag":"file","id":"id:f3","name":"new.md","path_display":"/new.md","server_modified":"2026-03-01T00:00:00Z","content_hash":"hash3"}
				],
				"cursor": "cursor-updated",
				"has_more": false
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "dropbox",
		"--access-token", "tok",
		"--dropbox-api-url", srv.URL,
		"--dropbox-content-url", srv.URL,
		"--cursor", "saved-cursor",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("cursor resume dry-run: %v", err)
	}

	if !strings.Contains(out, "cursor=<incremental>") {
		t.Fatalf("expected cursor scope description, got:\n%s", out)
	}
	if !strings.Contains(out, "Dropbox files matched: 1 (dry-run)") {
		t.Fatalf("expected 1 file from cursor resume, got:\n%s", out)
	}
}

func TestImportDropboxMaxItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2/files/list_folder":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{
				"entries": [
					{".tag":"file","id":"id:f1","name":"a.md","path_display":"/a.md","server_modified":"2026-01-01T00:00:00Z"},
					{".tag":"file","id":"id:f2","name":"b.md","path_display":"/b.md","server_modified":"2026-01-02T00:00:00Z"},
					{".tag":"file","id":"id:f3","name":"c.md","path_display":"/c.md","server_modified":"2026-01-03T00:00:00Z"}
				],
				"cursor": "c1",
				"has_more": false
			}`))
		case "/2/files/list_folder/get_latest_cursor":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"cursor":"c-latest"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand(
		"import", "dropbox",
		"--access-token", "tok",
		"--dropbox-api-url", srv.URL,
		"--dropbox-content-url", srv.URL,
		"--max-items", "2",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("max-items dry-run: %v", err)
	}

	if strings.Contains(out, "max-items=2") {
		t.Logf("scope contains max-items: %s", out)
	}
	// At most 2 files from the truncated list.
	if strings.Contains(out, "Dropbox files matched: 3") {
		t.Fatalf("expected max-items to cap result, but got 3 matches:\n%s", out)
	}
}

func init() {
	// Ensure importDropboxCmd is registered.
	_ = importDropboxCmd
}
