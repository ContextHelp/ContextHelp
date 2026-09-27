package idxbridge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

// mockInstance fakes one dpkms instance's health and search surfaces:
// togglable health, hit counters, configurable search status and an
// optional required bearer token.
type mockInstance struct {
	srv          *httptest.Server
	healthy      atomic.Bool
	healthHits   atomic.Int64
	searchHits   atomic.Int64
	jobID        string
	searchStatus int          // 0 → 200 OK
	requireToken string       // non-empty → search demands this bearer token
	lastAuth     atomic.Value // string: Authorization header of the last search
	healthAuth   atomic.Value // string: Authorization header of the last health probe
}

// checkAuth enforces requireToken on the API surfaces (never on /health).
// Returns false after writing the 401 when the bearer token is missing/wrong.
func (m *mockInstance) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	m.lastAuth.Store(r.Header.Get("Authorization"))
	if m.requireToken == "" {
		return true
	}
	if r.Header.Get("Authorization") == "Bearer "+m.requireToken {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"UNAUTHORIZED","message":"missing or invalid token"}`))
	return false
}

func newMockInstance(t *testing.T, jobID string) *mockInstance {
	t.Helper()
	m := &mockInstance{jobID: jobID}
	m.healthy.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		m.healthHits.Add(1)
		m.healthAuth.Store(r.Header.Get("Authorization"))
		if !m.healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		m.searchHits.Add(1)
		if !m.checkAuth(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if m.searchStatus != 0 && m.searchStatus != http.StatusOK {
			w.WriteHeader(m.searchStatus)
			_, _ = w.Write([]byte(`{"error":"BAD_QUERY","message":"unparseable RSQL"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":  []map[string]any{{"id": "obj-" + m.jobID, "type": "note"}},
			"total": 1,
		})
	})
	m.srv = httptest.NewServer(mux)
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mockInstance) url() string { return m.srv.URL }

// TestMultiServer_SearchFailover: the read surface walks the same ordered
// list — primary down routes the RSQL search to the next instance.
func TestMultiServer_SearchFailover(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "s")
	bridge := idxbridge.New(idxbridge.Config{
		BaseURLs:              []string{primary.url(), secondary.url()},
		ProbeTimeout:          200 * time.Millisecond,
		NegativeProbeInterval: 20 * time.Millisecond,
		Fallback:              &fakeFallback{},
	})

	objs, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if total != 1 || len(objs) != 1 || objs[0].ID != "obj-s" {
		t.Errorf("got %d objs (total %d); want the fallback instance's result", len(objs), total)
	}
	if primary.searchHits.Load() != 0 {
		t.Error("unhealthy primary received a search request")
	}
	if secondary.searchHits.Load() != 1 {
		t.Errorf("fallback instance search hits = %d; want 1", secondary.searchHits.Load())
	}
}

// TestMultiServer_BaseURLsWinOverBaseURL: when both are set, the ordered
// list is authoritative.
func TestMultiServer_BaseURLsWinOverBaseURL(t *testing.T) {
	listed := newMockInstance(t, "listed")
	ignored := newMockInstance(t, "ignored")
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:  ignored.url(),
		BaseURLs: []string{listed.url()},
		Fallback: &fakeFallback{},
	})

	objs, _, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if len(objs) != 1 || objs[0].ID != "obj-listed" {
		t.Errorf("got %+v; want the BaseURLs entry's result", objs)
	}
	if ignored.searchHits.Load() != 0 {
		t.Error("BaseURL received traffic despite BaseURLs being set")
	}
}
