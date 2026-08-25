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

// TestAnalyzePrimaryDownRoutesToFallbackInstance: with server.urls listing a
// dead primary and a live fallback instance, the write routes to the fallback
// instance — never the local direct path.
func TestAnalyzePrimaryDownRoutesToFallbackInstance(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := countingAnalyzeDaemon(t, "job_failover_1", &hits)
	cfgArgs := serverURLsConfig(t, deadServerURL, srv.URL)

	out, err := executeCommand(append(append([]string{"analyze", "failover content"}, cfgArgs...), storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze with live fallback instance: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_failover_1") {
		t.Errorf("output should carry the fallback instance's job ID: %q", out)
	}
	if hits.Load() != 1 {
		t.Errorf("fallback instance analyze hits = %d; want 1", hits.Load())
	}
	if strings.Contains(out, "Queued locally") {
		t.Errorf("no queued-locally notice when an instance accepted the write: %q", out)
	}
	if _, statErr := os.Stat(dbPath); statErr == nil {
		t.Error("local database opened despite a live instance; write must route via API")
	}
}

// TestAnalyzeAllInstancesDownFallsBackLocally: every listed instance dead —
// the write lands in the gated local queue.
func TestAnalyzeAllInstancesDownFallsBackLocally(t *testing.T) {
	dbPath := tempDB(t)
	cfgArgs := serverURLsConfig(t, deadServerURL, "http://127.0.0.1:19998")

	out, err := executeCommand(append(append([]string{"analyze", "fully offline"}, cfgArgs...), storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze with all instances down should fall back locally: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Errorf("output lacks Job ID: %q", out)
	}
	if !strings.Contains(out, "dpkms serve") {
		t.Errorf("output should carry the queued-locally notice: %q", out)
	}
}

// TestAnalyzeServerFlagPinsRouting: an explicit --server bypasses the
// configured list entirely — dead pinned instance means local fallback, even
// while a listed instance is live.
func TestAnalyzeServerFlagPinsRouting(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := countingAnalyzeDaemon(t, "job_should_not_serve", &hits)
	cfgArgs := serverURLsConfig(t, srv.URL)

	out, err := executeCommand(append(append([]string{"analyze", "pinned content", "--server", deadServerURL}, cfgArgs...), storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("analyze with pinned dead server should fall back locally: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("listed instance served %d requests despite --server pin", hits.Load())
	}
	if !strings.Contains(out, "Job ID:") {
		t.Errorf("output lacks Job ID: %q", out)
	}
	if !strings.Contains(out, "dpkms serve") {
		t.Errorf("output should carry the queued-locally notice: %q", out)
	}
}

// TestListQueryPrimaryDownFallsBackToSecondInstance: the read surface walks
// the same list — dead primary routes the RSQL search to the next instance.
func TestListQueryPrimaryDownFallsBackToSecondInstance(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := startMockSearchDaemon(t, &hits)
	defer srv.Close()
	cfgArgs := serverURLsConfig(t, deadServerURL, srv.URL)

	out, err := executeCommand(append(append([]string{"list", "--q", "type==note"}, cfgArgs...), storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("list --q with live second instance: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("second instance search hits = %d; want 1", hits.Load())
	}
	if !strings.Contains(out, "daemon-obj-1") {
		t.Errorf("output should contain the instance-served object: %q", out)
	}
}
