package idxbridge_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// dedupeStore models the server-side idempotency ledger shared by instances
// serving the same queue (e.g. two dpkms processes over one database): a
// key already admitted resolves to the existing job instead of enqueueing.
type dedupeStore struct {
	mu       sync.Mutex
	byKey    map[string]string // idempotency key → job id
	enqueues int
}

func newDedupeStore() *dedupeStore { return &dedupeStore{byKey: map[string]string{}} }

// admit records key→jobID unless the key is already present. Returns the
// surviving job id and whether this call actually enqueued.
func (d *dedupeStore) admit(key, jobID string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if key != "" {
		if existing, ok := d.byKey[key]; ok {
			return existing, false
		}
		d.byKey[key] = jobID
	}
	d.enqueues++
	return jobID, true
}

func (d *dedupeStore) enqueueCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.enqueues
}

// mockInstance fakes one dpkms instance: togglable health, hit counters,
// configurable analyze status, optional mid-request connection drop, and an
// optional shared dedupe ledger honoring the idempotency key.
type mockInstance struct {
	srv           *httptest.Server
	healthy       atomic.Bool
	analyzeHits   atomic.Int64
	searchHits    atomic.Int64
	jobID         string
	analyzeStatus int          // 0 → 202 Accepted
	dropAnalyze   bool         // enqueue, then close the conn instead of answering
	dedupe        *dedupeStore // nil → every accepted analyze enqueues
	lastKey       atomic.Value // string: idempotency key of the last analyze
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
		var req service.AnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		m.lastKey.Store(req.IdempotencyKey)

		status := m.analyzeStatus
		if status == 0 {
			status = http.StatusAccepted
		}
		if status != http.StatusAccepted {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"REJECTED","message":"instance refused"}`))
			return
		}

		// Accepted: the enqueue happens whether or not the response
		// survives the trip back.
		jobID, enqueued := m.jobID, true
		if m.dedupe != nil {
			jobID, enqueued = m.dedupe.admit(req.IdempotencyKey, m.jobID)
		}
		if m.dropAnalyze {
			// Response loss AFTER the body was delivered: the job is in
			// the queue, the client never learns its id.
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
		w.Header().Set("Content-Type", "application/json")
		if enqueued {
			w.WriteHeader(http.StatusAccepted)
		} else {
			w.WriteHeader(http.StatusOK) // replay resolved to the existing job
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
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

// TestMultiServer_TransportFailureWalksToNext: primary enqueues the payload
// but the response is lost mid-flight. The walk retries the next instance
// carrying the same idempotency key, so the replay resolves to the job the
// primary already enqueued — exactly one enqueue, no duplicate ingestion.
func TestMultiServer_TransportFailureWalksToNext(t *testing.T) {
	shared := newDedupeStore() // both instances serve the same queue
	primary := newMockInstance(t, "job-primary")
	primary.dropAnalyze = true
	primary.dedupe = shared
	secondary := newMockInstance(t, "job-secondary")
	secondary.dedupe = shared
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if servedBy != secondary.url() {
		t.Errorf("servedBy = %q; want the secondary %q after primary transport failure", servedBy, secondary.url())
	}
	if jobID != "job-primary" {
		t.Errorf("jobID = %q; want the primary's already-enqueued job (replay deduped)", jobID)
	}
	if got := shared.enqueueCount(); got != 1 {
		t.Errorf("enqueues = %d; want exactly 1 — the replay must not duplicate the payload", got)
	}
	if fb.called {
		t.Error("local fallback called while the fallback instance was live")
	}
}

// TestMultiServer_WalkCarriesSameIdempotencyKey: the key is minted once per
// logical submission — every attempt in the walk sends the identical,
// non-empty key.
func TestMultiServer_WalkCarriesSameIdempotencyKey(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.dropAnalyze = true
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := twoInstanceBridge(t, primary, secondary, fb)

	if _, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	k1, _ := primary.lastKey.Load().(string)
	k2, _ := secondary.lastKey.Load().(string)
	if k1 == "" {
		t.Fatal("no idempotency key sent to the primary; the bridge must mint one per submission")
	}
	if k1 != k2 {
		t.Errorf("replay key %q differs from original %q; the walk must carry the same key", k2, k1)
	}
}

// TestMultiServer_CallerSuppliedKeyPreserved: a caller that minted its own
// key keeps it — the bridge only fills the empty case.
func TestMultiServer_CallerSuppliedKeyPreserved(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURLs:        []string{primary.url()},
		AnalyzeFallback: fb,
	})

	_, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{
		Content: "x", Type: "text", IdempotencyKey: "caller-key-1",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got, _ := primary.lastKey.Load().(string); got != "caller-key-1" {
		t.Errorf("instance saw key %q; want the caller-supplied key preserved", got)
	}
}

// TestMultiServer_LocalFallbackCarriesKey: when every instance is down the
// local fallback receives the same keyed request, so a later daemon-side
// replay of the queued work stays deduplicable.
func TestMultiServer_LocalFallbackCarriesKey(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.healthy.Store(false)
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURLs:              []string{primary.url()},
		ProbeTimeout:          200 * time.Millisecond,
		NegativeProbeInterval: 20 * time.Millisecond,
		AnalyzeFallback:       fb,
	})

	if _, _, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"}); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !fb.called {
		t.Fatal("local fallback not called")
	}
	if fb.got.IdempotencyKey == "" {
		t.Error("local fallback received no idempotency key")
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
