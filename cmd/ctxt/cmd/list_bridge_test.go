package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/viper"
)

// startMockSearchDaemon fakes the daemon's health + search surface.
func startMockSearchDaemon(t *testing.T, hits *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "daemon-obj-1", "type": "note"},
			},
			"total": 1,
		})
	})
	return httptest.NewServer(mux)
}

// TestListQueryRoutesViaDaemon: `list --q` must route the RSQL search through
// a live daemon instead of querying local storage directly.
func TestListQueryRoutesViaDaemon(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := startMockSearchDaemon(t, &hits)
	defer srv.Close()

	viper.Set("server.url", srv.URL)
	t.Cleanup(func() { viper.Set("server.url", "") })

	out, err := executeCommand(append([]string{"list", "--q", "type==note"}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("list --q via live daemon: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("daemon search hit %d times; want 1 (routed via API)", hits.Load())
	}
	if !strings.Contains(out, "daemon-obj-1") {
		t.Errorf("output should contain the daemon-served object: %q", out)
	}
}

// TestListQueryFallsBackWhenDaemonDown: with no daemon, `list --q` answers
// from local storage (empty here) instead of failing.
func TestListQueryFallsBackWhenDaemonDown(t *testing.T) {
	dbPath := tempDB(t)

	viper.Set("server.url", deadServerURL)
	t.Cleanup(func() { viper.Set("server.url", "") })

	out, err := executeCommand(append([]string{"list", "--q", "type==note"}, storageOverride(dbPath)...)...)
	if err != nil {
		t.Fatalf("list --q with daemon down should fall back locally: %v", err)
	}
	if !strings.Contains(out, "No results") && !strings.Contains(out, "0") {
		t.Errorf("expected an empty local result set, got: %q", out)
	}
}
