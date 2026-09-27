package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/kit/go/console/output"
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

// TestListQueryRoutesViaDaemon: `list --q` sends the RSQL search to the
// resolved instance.
func TestListQueryRoutesViaDaemon(t *testing.T) {
	dbPath := tempDB(t)

	var hits atomic.Int64
	srv := startMockSearchDaemon(t, &hits)
	defer srv.Close()

	out, err := executeCommand(append([]string{"list", "--q", "type==note", "-c", "server.url=" + srv.URL}, storageOverride(dbPath)...)...)
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

// TestListQueryAnswersWithoutLocalStore: `list --q` answers from the
// instance and never opens the store the config names.
func TestListQueryAnswersWithoutLocalStore(t *testing.T) {
	db := setupTestDB(t)
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-note", Type: "note", Summaries: []string{"a note"}})
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-url", Type: "url"})
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("list", "--q", "type==note")
	if err != nil {
		t.Fatalf("list --q: %v\n%s", err, out)
	}
	if !strings.Contains(out, "o-note") || strings.Contains(out, "o-url") {
		t.Errorf("list --q type==note output:\n%s", out)
	}
	assertNoLocalStore(t, local)
}

// TestListQueryUnreachableHasNoLocalFallback: with nothing answering,
// `list --q` exits 70 and never searches (or creates) a local store.
func TestListQueryUnreachableHasNoLocalFallback(t *testing.T) {
	db := setupTestDB(t, dpkmstest.Unreachable())
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("list", "--q", "type==note")
	if got := exitOf(err); got != output.ExitPrerequisite {
		t.Fatalf("exit %d (%v); want 70\n%s", got, err, out)
	}
	assertNoLocalStore(t, local)
}

// TestListQueryRejectedTokenHasNoLocalFallback: a 401 exits 5 after one
// request and searches nothing locally.
func TestListQueryRejectedTokenHasNoLocalFallback(t *testing.T) {
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedObj(t, db, &storage.KnowledgeObject{ID: "o-note", Type: "note"})
	db.useRole(t, "none")
	local := pointStorageElsewhere(t, db)

	out, err := db.exec("list", "--q", "type==note")
	if got := exitOf(err); got != output.ExitUnauthorized {
		t.Fatalf("exit %d (%v); want 5\n%s", got, err, out)
	}
	if strings.Contains(out, "o-note") {
		t.Errorf("a rejected search printed results:\n%s", out)
	}
	assertNoLocalStore(t, local)
}

// TestListQueryMalformedIsUsage: the instance refuses a query it cannot
// parse (400), and list exits 2.
func TestListQueryMalformedIsUsage(t *testing.T) {
	db := setupTestDB(t)
	out, err := db.exec("list", "--q", "type=bogus=(")
	if got := exitOf(err); got != output.ExitUsage {
		t.Fatalf("exit %d (%v); want 2\n%s", got, err, out)
	}
}
