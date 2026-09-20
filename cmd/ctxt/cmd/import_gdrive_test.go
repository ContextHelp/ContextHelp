package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestImportGDriveRequiresAccessToken(t *testing.T) {
	_, err := executeCommand("import", "gdrive", "--dry-run")
	if err == nil {
		t.Fatal("expected missing token to fail")
	}
	if !strings.Contains(err.Error(), "google drive token is required") {
		t.Fatalf("expected token guidance, got: %v", err)
	}
}

func TestImportGDriveDryRunScopeAndSelection(t *testing.T) {
	var (
		mu     sync.Mutex
		seenQ  string
		seenAu string
	)

	driveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v3/files" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		seenQ = r.URL.Query().Get("q")
		seenAu = r.Header.Get("Authorization")
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"files": [
				{"id":"f1","name":"Q1 Plan","mimeType":"application/vnd.google-apps.document","modifiedTime":"2026-01-15T12:00:00Z","webViewLink":"https://drive.google.com/file/d/f1/view"},
				{"id":"f2","name":"Q1 PDF","mimeType":"application/pdf","modifiedTime":"2026-01-20T12:00:00Z","webViewLink":"https://drive.google.com/file/d/f2/view"}
			]
		}`))
	}))
	defer driveSrv.Close()

	out, err := executeCommand(
		"import", "gdrive",
		"--access-token", "token_123",
		"--drive-base-url", driveSrv.URL,
		"--since", "2026-01-01",
		"--folder-id", "folder-abc",
		"--mime-type", "application/pdf",
		"--query", "name contains 'Q1'",
		"--max-items", "5",
		"--dry-run",
	)
	if err != nil {
		t.Fatalf("import gdrive dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "GDrive scope:") {
		t.Fatalf("expected scope output, got:\n%s", out)
	}
	if !strings.Contains(out, "GDrive files matched: 2 (dry-run)") {
		t.Fatalf("expected dry-run count, got:\n%s", out)
	}
	if !strings.Contains(out, "since=2026-01-01T00:00:00Z") {
		t.Fatalf("expected since scope value, got:\n%s", out)
	}

	mu.Lock()
	gotQ := seenQ
	gotAuth := seenAu
	mu.Unlock()

	if gotAuth != "Bearer token_123" {
		t.Fatalf("expected auth header, got %q", gotAuth)
	}
	for _, fragment := range []string{
		"trashed = false",
		"modifiedTime >=",
		"'folder-abc' in parents",
		"mimeType = 'application/pdf'",
		"(name contains 'Q1')",
	} {
		if !strings.Contains(gotQ, fragment) {
			t.Fatalf("expected query fragment %q in %q", fragment, gotQ)
		}
	}
}

func TestImportGDriveEnqueueSuccess(t *testing.T) {
	driveSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/files":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"files": [
					{"id":"doc-1","name":"Design Doc","mimeType":"application/vnd.google-apps.document","modifiedTime":"2026-01-15T12:00:00Z","webViewLink":"https://drive.google.com/file/d/doc-1/view"},
					{"id":"pdf-1","name":"Spec PDF","mimeType":"application/pdf","modifiedTime":"2026-01-20T12:00:00Z","webViewLink":"https://drive.google.com/file/d/pdf-1/view"}
				]
			}`))
		case "/drive/v3/files/doc-1/export":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("This is the exported document body."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer driveSrv.Close()

	var (
		mu          sync.Mutex
		textCount   int
		urlCount    int
		sourceCount int
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
		switch req.Type {
		case "text":
			textCount++
		case "url":
			urlCount++
		}
		if req.Source == "import:gdrive" {
			sourceCount++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"job_gdrive_123"}`))
	}))
	defer dpkmsSrv.Close()

	out, err := executeCommand(
		"import", "gdrive",
		"--access-token", "token_123",
		"--drive-base-url", driveSrv.URL,
		"--server", dpkmsSrv.URL,
	)
	if err != nil {
		t.Fatalf("import gdrive should succeed: %v", err)
	}

	if !strings.Contains(out, "GDrive files scanned: 2") {
		t.Fatalf("expected scanned summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Imported: 2") {
		t.Fatalf("expected imported summary, got:\n%s", out)
	}

	mu.Lock()
	gotText := textCount
	gotURL := urlCount
	gotSource := sourceCount
	mu.Unlock()

	if gotText != 1 || gotURL != 1 {
		t.Fatalf("expected one text and one url enqueue, got text=%d url=%d", gotText, gotURL)
	}
	if gotSource != 2 {
		t.Fatalf("expected source metadata on both requests, got %d", gotSource)
	}
}
