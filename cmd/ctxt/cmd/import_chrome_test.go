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

const chromeBookmarksFixture = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><H3>Toolbar</H3>
    <DL><p>
        <DT><A HREF="https://example.com">Example</A>
        <DT><A HREF="https://go.dev">Go</A>
    </DL><p>
</DL><p>`

func TestImportChromeDryRun(t *testing.T) {
	file := writeTempBookmarks(t, chromeBookmarksFixture)

	out, err := executeCommand("import", "chrome", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import chrome dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Chrome bookmarks parsed: 2") {
		t.Fatalf("expected parsed count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Toolbar") {
		t.Fatalf("expected folder preview in output, got:\n%s", out)
	}
}

func TestImportChromeEnqueueSuccess(t *testing.T) {
	file := writeTempBookmarks(t, chromeBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/enqueue" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_123"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "chrome", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import chrome should succeed: %v", err)
	}

	if !strings.Contains(out, "Chrome bookmarks processed: 2") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected enqueued summary, got:\n%s", out)
	}
}

func TestImportChromeFallbackToAnalyzeEndpoint(t *testing.T) {
	file := writeTempBookmarks(t, chromeBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pipelines/enqueue":
			http.NotFound(w, r)
		case "/api/v1/analyze":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_legacy"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("import", "chrome", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import chrome fallback should succeed: %v", err)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected fallback enqueue success, got:\n%s", out)
	}
}

func TestImportChromeRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "chrome")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}

func TestImportChromeEmptyFileError(t *testing.T) {
	file := writeTempBookmarks(t, "  \n")

	_, err := executeCommand("import", "chrome", "--file", file, "--dry-run")
	if err == nil {
		t.Fatal("expected empty file to fail")
	}
}

func writeTempBookmarks(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "bookmarks.html")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write bookmarks fixture: %v", err)
	}
	return path
}
