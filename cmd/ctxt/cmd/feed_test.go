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
	for _, subcmd := range []string{"add", "list", "sync", "delete"} {
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

	out, err := executeCommand("feed", "list", "--server", srv.URL, "--format", "json")
	if err != nil {
		t.Fatalf("feed list --format json should succeed: %v", err)
	}
	if !strings.Contains(out, `"feeds"`) {
		t.Errorf("JSON output should contain feeds key, got: %s", out)
	}
}

func TestFeedSyncByID(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "sync", "feed_001", "--server", srv.URL)
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

	out, err := executeCommand("feed", "sync", "https://example.com/feed.xml", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed sync by URL should succeed: %v", err)
	}
	if !strings.Contains(out, "job_sync001") {
		t.Errorf("output should contain sync job ID, got: %s", out)
	}
}

// TestFeedSyncTakesOneOperand: feed sync names its feed positionally;
// none, two, or the removed --id/--url flags are usage errors.
func TestFeedSyncTakesOneOperand(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()
	for _, args := range [][]string{
		{"feed", "sync"},
		{"feed", "sync", "feed_001", "feed_002"},
		{"feed", "sync", "--id", "feed_001"},
		{"feed", "sync", "--url", "https://example.com/feed.xml"},
	} {
		_, err := executeCommand(append(args, "--server", srv.URL)...)
		if err == nil || !(strings.Contains(err.Error(), "accepts 1 arg") || strings.Contains(err.Error(), "unknown flag")) {
			t.Errorf("%v: err = %v, want an arity or unknown-flag error", args, err)
		}
	}
}

// TestFeedSyncUnknownURL: a URL no subscription has is an error, not a
// sync of an ID spelled like a URL.
func TestFeedSyncUnknownURL(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()
	_, err := executeCommand("feed", "sync", "https://nowhere.example/rss", "--server", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "no feed found") {
		t.Fatalf("err = %v, want no feed found", err)
	}
}

func TestFeedRemoveByID(t *testing.T) {
	srv := startMockFeedServer(t)
	defer srv.Close()

	out, err := executeCommand("feed", "delete", "feed_001", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed delete by ID should succeed: %v", err)
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

	out, err := executeCommand("feed", "delete", "https://example.com/feed.xml", "--server", srv.URL)
	if err != nil {
		t.Fatalf("feed delete by URL should succeed: %v", err)
	}
	if !strings.Contains(out, "removed") {
		t.Errorf("output should confirm removal, got: %s", out)
	}
}

func TestFeedRemoveRequiresArg(t *testing.T) {
	_, err := executeCommand("feed", "delete")
	if err == nil {
		t.Error("feed delete without arg should fail")
	}
}

// TestE2EFeedSyncUsage: on the built binary the removed flags and a
// missing operand are usage errors (exit 2).
func TestE2EFeedSyncUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	for _, args := range [][]string{
		{"feed", "sync"},
		{"feed", "sync", "--id", "feed_001"},
		{"feed", "sync", "--url", "https://example.com/feed.xml"},
	} {
		if got, out := runBuiltCtxt(t, args...); got != 2 {
			t.Errorf("ctxt %v: exit %d, want 2\n%s", args, got, out)
		}
	}
}
