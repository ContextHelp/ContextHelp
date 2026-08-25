package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// startTokenDPKMS starts a mock dpkms that requires the given bearer token
// on the API surfaces (/health stays public) and records the Authorization
// header seen on the job poll.
func startTokenDPKMS(t *testing.T, token string, pollAuth *atomic.Value) *httptest.Server {
	t.Helper()
	authorized := func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer "+token
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/v1/analyze" && r.Method == http.MethodPost {
			if !authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": "job_authed_1"})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") && r.Method == http.MethodGet {
			if pollAuth != nil {
				pollAuth.Store(r.Header.Get("Authorization"))
			}
			if !authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "job_authed_1", "status": "done"})
			return
		}
		http.NotFound(w, r)
	}))
}

// writeServerConfig renders a config file wiring one credentialed endpoint.
func writeServerConfig(t *testing.T, url, token string) string {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  urls:\n    - url: " + url + "\n      token: " + token + "\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

// TestAnalyzeSendsConfiguredToken: a server.urls entry with a token routes
// the analyze POST with that bearer token — the credentialed instance
// accepts, no local fallback.
func TestAnalyzeSendsConfiguredToken(t *testing.T) {
	dbPath := tempDB(t)
	srv := startTokenDPKMS(t, "tok-cmd-1", nil)
	defer srv.Close()
	cfgPath := writeServerConfig(t, srv.URL, "tok-cmd-1")

	out, err := executeCommand("analyze", "credentialed content",
		"-c", cfgPath, "-c", "storage.path="+dbPath)
	if err != nil {
		t.Fatalf("analyze with configured token: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_authed_1") {
		t.Errorf("output should carry the instance's job ID: %q", out)
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("local database opened despite the credentialed instance accepting")
	}
}

// TestAnalyzeWaitPollSendsToken: the --wait job poll authenticates against
// the instance that accepted the enqueue.
func TestAnalyzeWaitPollSendsToken(t *testing.T) {
	dbPath := tempDB(t)
	var pollAuth atomic.Value
	srv := startTokenDPKMS(t, "tok-cmd-2", &pollAuth)
	defer srv.Close()
	cfgPath := writeServerConfig(t, srv.URL, "tok-cmd-2")

	out, err := executeCommand("analyze", "watched content", "--wait",
		"-c", cfgPath, "-c", "storage.path="+dbPath)
	if err != nil {
		t.Fatalf("analyze --wait with configured token: %v (out=%q)", err, out)
	}
	if got, _ := pollAuth.Load().(string); got != "Bearer tok-cmd-2" {
		t.Errorf("job poll Authorization = %q; want Bearer tok-cmd-2", got)
	}
}

// TestAnalyzeServerFlagHelpMentionsPinning: --server pins/bypasses the
// configured list; the help text must say so instead of citing a stale
// default.
func TestAnalyzeServerFlagHelpMentionsPinning(t *testing.T) {
	out, err := executeCommand("analyze", "--help")
	if err != nil {
		t.Fatalf("analyze --help: %v", err)
	}
	if !strings.Contains(out, "pin") || !strings.Contains(out, "server.urls") {
		t.Errorf("--server help must explain pinning/bypass of server.urls; got %q", out)
	}
	if strings.Contains(out, "http://localhost:8080") {
		t.Errorf("--server help still cites the stale default: %q", out)
	}
}
