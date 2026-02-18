package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const safariBookmarksFixture = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><H3>Favorites</H3>
    <DL><p>
        <DT><A HREF="https://apple.com">Apple</A>
        <DT><A HREF="https://developer.apple.com">Apple Developer</A>
    </DL><p>
</DL><p>`

func TestImportSafariDryRun(t *testing.T) {
	file := writeTempBookmarks(t, safariBookmarksFixture)

	out, err := executeCommand("import", "safari", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import safari dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Safari bookmarks parsed: 2") {
		t.Fatalf("expected parsed count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Favorites") {
		t.Fatalf("expected folder preview in output, got:\n%s", out)
	}
}

func TestImportSafariEnqueueSuccess(t *testing.T) {
	file := writeTempBookmarks(t, safariBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/enqueue" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_safari_123"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "safari", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import safari should succeed: %v", err)
	}

	if !strings.Contains(out, "Safari bookmarks processed: 2") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected enqueued summary, got:\n%s", out)
	}
}

func TestImportSafariFallbackToAnalyzeEndpoint(t *testing.T) {
	file := writeTempBookmarks(t, safariBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pipelines/enqueue":
			http.NotFound(w, r)
		case "/api/v1/analyze":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_safari_legacy"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("import", "safari", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import safari fallback should succeed: %v", err)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected fallback enqueue success, got:\n%s", out)
	}
}

func TestImportSafariRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "safari")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}
