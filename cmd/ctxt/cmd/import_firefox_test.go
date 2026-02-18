package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const firefoxBookmarksFixture = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><H3>Bookmarks Toolbar</H3>
    <DL><p>
        <DT><A HREF="https://www.mozilla.org">Mozilla</A>
        <DT><A HREF="https://support.mozilla.org">Firefox Support</A>
    </DL><p>
</DL><p>`

func TestImportFirefoxDryRun(t *testing.T) {
	file := writeTempBookmarks(t, firefoxBookmarksFixture)

	out, err := executeCommand("import", "firefox", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import firefox dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Firefox bookmarks parsed: 2") {
		t.Fatalf("expected parsed count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Bookmarks Toolbar") {
		t.Fatalf("expected folder preview in output, got:\n%s", out)
	}
}

func TestImportFirefoxEnqueueSuccess(t *testing.T) {
	file := writeTempBookmarks(t, firefoxBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/enqueue" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_firefox_123"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "firefox", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import firefox should succeed: %v", err)
	}

	if !strings.Contains(out, "Firefox bookmarks processed: 2") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected enqueued summary, got:\n%s", out)
	}
}

func TestImportFirefoxFallbackToAnalyzeEndpoint(t *testing.T) {
	file := writeTempBookmarks(t, firefoxBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pipelines/enqueue":
			http.NotFound(w, r)
		case "/api/v1/analyze":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_firefox_legacy"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("import", "firefox", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import firefox fallback should succeed: %v", err)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected fallback enqueue success, got:\n%s", out)
	}
}

func TestImportFirefoxRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "firefox")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}
