package idxbridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// mockInstance fakes one dpkms instance: togglable health, hit counters,
// configurable analyze status, optional mid-request connection drop.
type mockInstance struct {
	srv           *httptest.Server
	healthy       atomic.Bool
	analyzeHits   atomic.Int64
	searchHits    atomic.Int64
	jobID         string
	analyzeStatus int  // 0 → 202 Accepted
	dropAnalyze   bool // hijack + close the conn instead of answering
}

func newMockInstance(t *testing.T, jobID string) *mockInstance {
	t.Helper()
	m := &mockInstance{jobID: jobID}
	m.healthy.Store(true)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !m.healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/v1/analyze", func(w http.ResponseWriter, r *http.Request) {
		m.analyzeHits.Add(1)
		if m.dropAnalyze {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("hijacking unsupported")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		status := m.analyzeStatus
		if status == 0 {
			status = http.StatusAccepted
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status == http.StatusAccepted {
			_ = json.NewEncoder(w).Encode(map[string]string{"job_id": m.jobID})
		} else {
			_, _ = w.Write([]byte(`{"error":"REJECTED","message":"instance refused"}`))
		}
	})
	mux.HandleFunc("/api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		m.searchHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
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

func twoInstanceBridge(t *testing.T, primary, secondary *mockInstance, fb *fakeAnalyzer) *idxbridge.IdxBridge {
	t.Helper()
	return idxbridge.New(idxbridge.Config{
		BaseURLs:              []string{primary.url(), secondary.url()},
		ProbeTimeout:          200 * time.Millisecond,
		NegativeProbeInterval: 20 * time.Millisecond,
		AnalyzeFallback:       fb,
		Fallback:              &fakeFallback{},
	})
}

// TestMultiServer_PrimaryPreferred: both instances live — everything routes
// to the first URL in the list, the fallback instance stays untouched.
func TestMultiServer_PrimaryPreferred(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-primary" || servedBy != primary.url() {
		t.Errorf("routed to %q (job %q); want primary %q", servedBy, jobID, primary.url())
	}
	if secondary.analyzeHits.Load() != 0 {
		t.Error("fallback instance received traffic while primary live")
	}
	if fb.called {
		t.Error("local fallback called while primary live")
	}
}

// TestMultiServer_FailoverToSecondary: primary unhealthy — requests route to
// the next instance in order, never the local fallback.
func TestMultiServer_FailoverToSecondary(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-secondary" || servedBy != secondary.url() {
		t.Errorf("routed to %q (job %q); want secondary %q", servedBy, jobID, secondary.url())
	}
	if primary.analyzeHits.Load() != 0 {
		t.Error("unhealthy primary received an analyze request")
	}
	if fb.called {
		t.Error("local fallback called while an instance was live")
	}
}

// TestMultiServer_AllDownLocalFallback: no instance answers — the local
// fallback takes the write, servedBy is empty.
func TestMultiServer_AllDownLocalFallback(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "job-secondary")
	secondary.healthy.Store(false)
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-local" || servedBy != "" {
		t.Errorf("jobID=%q servedBy=%q; want local fallback with empty servedBy", jobID, servedBy)
	}
	if !fb.called {
		t.Fatal("local fallback not called with all instances down")
	}
}

// TestMultiServer_PrimaryResumes: after the primary recovers, traffic returns
// to it as soon as the (short) negative probe TTL lapses — per-request
// routing, no sticky failover.
func TestMultiServer_PrimaryResumes(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	ctx := context.Background()
	if _, servedBy, err := bridge.Analyze(ctx, service.AnalyzeRequest{Content: "x", Type: "text"}); err != nil || servedBy != secondary.url() {
		t.Fatalf("while primary down: servedBy=%q err=%v; want secondary", servedBy, err)
	}

	primary.healthy.Store(true)
	time.Sleep(50 * time.Millisecond) // > NegativeProbeInterval (20ms)

	_, servedBy, err := bridge.Analyze(ctx, service.AnalyzeRequest{Content: "y", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze after recovery: %v", err)
	}
	if servedBy != primary.url() {
		t.Errorf("routed to %q after primary recovery; want primary %q", servedBy, primary.url())
	}
}

// TestMultiServer_LiveRejectionNoFailover: a live instance's rejection is a
// decision — it must surface, not be replayed against the next instance or
// the local queue.
func TestMultiServer_LiveRejectionNoFailover(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.analyzeStatus = http.StatusUnprocessableEntity
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	_, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "document"})
	if err == nil {
		t.Fatal("Analyze succeeded; want live instance's rejection surfaced")
	}
	var rerr *idxbridge.RemoteError
	if !errors.As(err, &rerr) || rerr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("error = %v; want *RemoteError 422", err)
	}
	if secondary.analyzeHits.Load() != 0 {
		t.Error("rejected request replayed against the fallback instance")
	}
	if fb.called {
		t.Error("rejected request replayed against the local fallback")
	}
}

// TestMultiServer_TransportFailureWalksToNext: primary answers health but
// drops the request mid-flight — transport failure walks to the next
// instance and invalidates the primary's probe.
func TestMultiServer_TransportFailureWalksToNext(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.dropAnalyze = true
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-secondary" || servedBy != secondary.url() {
		t.Errorf("routed to %q (job %q); want secondary after primary transport failure", servedBy, jobID)
	}
	if fb.called {
		t.Error("local fallback called while the fallback instance was live")
	}
}

// TestMultiServer_SearchFailover: the read surface walks the same ordered
// list — primary down routes the RSQL search to the next instance.
func TestMultiServer_SearchFailover(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "s")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

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
	listed := newMockInstance(t, "job-listed")
	ignored := newMockInstance(t, "job-ignored")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:         ignored.url(),
		BaseURLs:        []string{listed.url()},
		AnalyzeFallback: fb,
	})

	_, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if servedBy != listed.url() {
		t.Errorf("routed to %q; want the BaseURLs entry %q", servedBy, listed.url())
	}
	if ignored.analyzeHits.Load() != 0 {
		t.Error("BaseURL received traffic despite BaseURLs being set")
	}
}
