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

	"hop.top/kit/go/console/output"
)

// serverURLsConfig writes a config file routing clients at the given ordered
// instance list (server.urls). Append the returned -c args to the command
// under test — the typed config, not viper state, is the routing source.
func serverURLsConfig(t *testing.T, urls ...string) []string {
	t.Helper()
	var b strings.Builder
	b.WriteString("server:\n  urls:\n")
	for _, u := range urls {
		b.WriteString("    - " + u + "\n")
	}
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return []string{"-c", cfgPath}
}

// countingAnalyzeDaemon fakes a dpkms instance's health + analyze surface and
// counts analyze hits.
func countingAnalyzeDaemon(t *testing.T, jobID string, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/analyze", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestAnalyzeNeverFailsOverToSecondInstance: with server.urls listing a
// dead first entry and a live second one, the write never reaches the
// second: every invocation talks to exactly one endpoint, and the dead
// one is PREREQUISITE (exit 70).
func TestAnalyzeNeverFailsOverToSecondInstance(t *testing.T) {
	isolateDataDirs(t)
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := countingAnalyzeDaemon(t, "job_failover_1", &hits)
	cfgArgs := serverURLsConfig(t, deadServerURL, srv.URL)

	out, err := executeCommand(append(append([]string{"analyze", "failover content"}, cfgArgs...), storageOverride(dbPath)...)...)
	assertKitExit(t, err, output.CodePrerequisite, output.ExitPrerequisite)
	assertNoLocalDB(t, filepath.Dir(dbPath))
	if hits.Load() != 0 {
		t.Errorf("second server.urls entry served %d requests; want 0 (no failover)", hits.Load())
	}
	if strings.Contains(out, "job_failover_1") {
		t.Errorf("output carries the second instance's job ID: %q", out)
	}
}

// TestAnalyzeServerFlagPinsRouting: an explicit --server bypasses the
// configured list entirely: a dead pinned instance is exit 70 even while
// a listed instance is live.
func TestAnalyzeServerFlagPinsRouting(t *testing.T) {
	isolateDataDirs(t)
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := countingAnalyzeDaemon(t, "job_should_not_serve", &hits)
	cfgArgs := serverURLsConfig(t, srv.URL)

	out, err := executeCommand(append(append([]string{"analyze", "pinned content", "--server", deadServerURL}, cfgArgs...), storageOverride(dbPath)...)...)
	assertKitExit(t, err, output.CodePrerequisite, output.ExitPrerequisite)
	if hits.Load() != 0 {
		t.Errorf("listed instance served %d requests despite --server pin", hits.Load())
	}
	if strings.Contains(out, "Job ID:") {
		t.Errorf("output claims an enqueue: %q", out)
	}
	assertNoLocalDB(t, filepath.Dir(dbPath))
}

// TestListQueryNeverFailsOverToSecondInstance: the read surface talks
// to the one resolved endpoint; a dead first entry never routes the RSQL
// search to the next instance.
func TestListQueryNeverFailsOverToSecondInstance(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := startMockSearchDaemon(t, &hits)
	defer srv.Close()
	cfgArgs := serverURLsConfig(t, deadServerURL, srv.URL)

	_, _ = executeCommand(append(append([]string{"list", "--q", "type==note"}, cfgArgs...), storageOverride(dbPath)...)...)
	if hits.Load() != 0 {
		t.Fatalf("second instance search hits = %d; want 0 (no failover)", hits.Load())
	}
}
