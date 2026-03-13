package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startMockFeedServer starts a mock server that handles feed API endpoints.
func startMockFeedServer(t *testing.T) *httptest.Server {
	t.Helper()
	feeds := []feedResponse{
		{ID: "feed_001", URL: "https://example.com/feed.xml", Status: "active", LastSync: "2026-03-13 10:00"},
		{ID: "feed_002", URL: "https://blog.example.com/rss", Status: "paused", LastSync: "2026-03-12 08:30"},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/feeds":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]string{"id": "feed_new123"})

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/feeds":
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(feeds)

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sync"):
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(map[string]string{"job_id": "job_sync001"})

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/feeds/"):
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
}

func TestFeedHelp(t *testing.T) {
	out, err := executeCommand("feed", "--help")
	if err != nil {
		t.Fatalf("feed --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"add", "list", "sync", "remove"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("feed help should list subcommand %q", subcmd)
		}
	}
}

func TestFeedAddHelp(t *testing.T) {
	out, err := executeCommand("feed", "add", "--help")
	if err != nil {
		t.Fatalf("feed add --help should succeed: %v", err)
	}
	if !strings.Contains(out, "<url>") {
		t.Error("feed add help should mention <url> argument")
	}
}

func TestFeedAddSuccess(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "add", "https://example.com/feed.xml", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed add should succeed: %v", err)
	}
	if !strings.Contains(out, "feed_new123") {
		t.Errorf("output should contain feed ID, got: %s", out)
	}
}

func TestFeedAddRequiresURL(t *testing.T) {
	_, err := executeCommand("feed", "add")
	if err == nil {
		t.Error("feed add without URL should fail")
	}
}

func TestFeedListSuccess(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "list", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed list should succeed: %v", err)
	}
	if !strings.Contains(out, "feed_001") {
		t.Errorf("output should contain feed ID, got: %s", out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("output should contain feed URL, got: %s", out)
	}
}

func TestFeedListJSONOutput(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "list", "--server", srv.URL, "--output", "json")
	if err != nil {
		t.Fatalf("feed list --output json should succeed: %v", err)
	}
	if !strings.Contains(out, `"feeds"`) {
		t.Errorf("JSON output should contain feeds key, got: %s", out)
	}
}

func TestFeedSyncByID(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "sync", "--id", "feed_001", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed sync by ID should succeed: %v", err)
	}
	if !strings.Contains(out, "job_sync001") {
		t.Errorf("output should contain sync job ID, got: %s", out)
	}
}

func TestFeedSyncByURL(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "sync", "--url", "https://example.com/feed.xml", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed sync by URL should succeed: %v", err)
	}
	if !strings.Contains(out, "job_sync001") {
		t.Errorf("output should contain sync job ID, got: %s", out)
	}
}

func TestFeedSyncRequiresIDOrURL(t *testing.T) {
	_, err := executeCommand("feed", "sync")
	if err == nil {
		t.Error("feed sync without --id or --url should fail")
	}
}

func TestFeedRemoveByID(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "remove", "feed_001", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed remove by ID should succeed: %v", err)
	}
	if !strings.Contains(out, "feed_001") {
		t.Errorf("output should mention feed ID, got: %s", out)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("output should confirm removal, got: %s", out)
	}
}

func TestFeedRemoveByURL(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "remove", "https://example.com/feed.xml", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed remove by URL should succeed: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("output should confirm removal, got: %s", out)
	}
}

func TestFeedRemoveRequiresArg(t *testing.T) {
	_, err := executeCommand("feed", "remove")
	if err == nil {
		t.Error("feed remove without arg should fail")
	}
}
