package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const edgeBookmarksFixture = `<!DOCTYPE NETSCAPE-Bookmark-file-1>
<DL><p>
    <DT><H3>Favorites bar</H3>
    <DL><p>
        <DT><A HREF="https://example.com">Example</A>
        <DT><A HREF="https://learn.microsoft.com">Microsoft Learn</A>
    </DL><p>
</DL><p>`

func TestImportEdgeDryRun(t *testing.T) {
	file := writeTempBookmarks(t, edgeBookmarksFixture)

	out, err := executeCommand("import", "edge", "--file", file, "--dry-run")
	if err != nil {
		t.Fatalf("import edge dry-run should succeed: %v", err)
	}

	if !strings.Contains(out, "Edge bookmarks parsed: 2") {
		t.Fatalf("expected parsed count in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Favorites bar") {
		t.Fatalf("expected folder preview in output, got:\n%s", out)
	}
}

func TestImportEdgeEnqueueSuccess(t *testing.T) {
	file := writeTempBookmarks(t, edgeBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pipelines/enqueue" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_edge_123"})
	}))
	defer srv.Close()

	out, err := executeCommand("import", "edge", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import edge should succeed: %v", err)
	}

	if !strings.Contains(out, "Edge bookmarks processed: 2") {
		t.Fatalf("expected processed summary, got:\n%s", out)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected enqueued summary, got:\n%s", out)
	}
}

func TestImportEdgeFallbackToAnalyzeEndpoint(t *testing.T) {
	file := writeTempBookmarks(t, edgeBookmarksFixture)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pipelines/enqueue":
			http.NotFound(w, r)
		case "/api/v1/analyze":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_edge_legacy"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	out, err := executeCommand("import", "edge", "--file", file, "--server", srv.URL)
	if err != nil {
		t.Fatalf("import edge fallback should succeed: %v", err)
	}
	if !strings.Contains(out, "Jobs enqueued: 2") {
		t.Fatalf("expected fallback enqueue success, got:\n%s", out)
	}
}

func TestImportEdgeRequiresFile(t *testing.T) {
	_, err := executeCommand("import", "edge")
	if err == nil {
		t.Fatal("expected missing --file to fail")
	}
}
