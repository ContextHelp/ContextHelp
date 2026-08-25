package idxbridge_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// TestAuth_AnalyzeSendsBearer: an endpoint configured with a token
// authenticates the analyze POST with it.
func TestAuth_AnalyzeSendsBearer(t *testing.T) {
	inst := newMockInstance(t, "job-1")
	inst.requireToken = "tok-1"
	fb := &fakeAnalyzer{jobID: "job-local"}
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:       []idxbridge.Endpoint{{URL: inst.url(), Token: "tok-1"}},
		AnalyzeFallback: fb,
	})

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-1" || servedBy != inst.url() {
		t.Errorf("routed to %q (job %q); want the credentialed instance", servedBy, jobID)
	}
	if got, _ := inst.lastAuth.Load().(string); got != "Bearer tok-1" {
		t.Errorf("Authorization = %q; want Bearer tok-1", got)
	}
	if fb.called {
		t.Error("local fallback used despite valid credentials")
	}
}

// TestAuth_SearchSendsBearer: the search GET carries the token too.
func TestAuth_SearchSendsBearer(t *testing.T) {
	inst := newMockInstance(t, "s")
	inst.requireToken = "tok-2"
	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints: []idxbridge.Endpoint{{URL: inst.url(), Token: "tok-2"}},
		Fallback:  fb,
	})

	objs, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if total != 1 || len(objs) != 1 {
		t.Errorf("got %d objs (total %d); want the instance's result", len(objs), total)
	}
	if got, _ := inst.lastAuth.Load().(string); got != "Bearer tok-2" {
		t.Errorf("Authorization = %q; want Bearer tok-2", got)
	}
	if fb.called {
		t.Error("local fallback used despite valid credentials")
	}
}

// TestAuth_ProbeStaysUnauthenticated: /health is public — the probe never
// carries the token even when one is configured.
func TestAuth_ProbeStaysUnauthenticated(t *testing.T) {
	inst := newMockInstance(t, "p")
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:       []idxbridge.Endpoint{{URL: inst.url(), Token: "tok-3"}},
		AnalyzeFallback: &fakeAnalyzer{},
	})

	if !bridge.Probe(context.Background()) {
		t.Fatal("Probe: instance should be reachable")
	}
	if got, _ := inst.healthAuth.Load().(string); got != "" {
		t.Errorf("health probe sent Authorization %q; probes must stay unauthenticated", got)
	}
}

// TestAuth_AnalyzeAuthRejectionWalks: 401/403 on analyze happens in
// middleware BEFORE enqueue, so the replay is safe — warn (naming the
// instance) and try the next one; do not surface, do not invalidate the
// probe.
func TestAuth_AnalyzeAuthRejectionWalks(t *testing.T) {
	primary := newMockInstance(t, "job-primary")
	primary.requireToken = "right-token" // bridge sends none → 401
	secondary := newMockInstance(t, "job-secondary")
	fb := &fakeAnalyzer{jobID: "job-local"}
	var warns bytes.Buffer
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints: []idxbridge.Endpoint{
			{URL: primary.url()},
			{URL: secondary.url()},
		},
		NegativeProbeInterval: 20 * time.Millisecond,
		AnalyzeFallback:       fb,
		WarnWriter:            &warns,
	})

	ctx := context.Background()
	jobID, servedBy, err := bridge.Analyze(ctx, service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if jobID != "job-secondary" || servedBy != secondary.url() {
		t.Errorf("routed to %q (job %q); want the next instance after a credential rejection", servedBy, jobID)
	}
	if fb.called {
		t.Error("local fallback used while the next instance was live")
	}
	if w := warns.String(); !strings.Contains(w, primary.url()) || !strings.Contains(w, "401") {
		t.Errorf("warning %q must name the rejecting instance and status", w)
	}

	// The probe must survive the 401: a second call re-attempts the
	// primary's analyze without a fresh health round-trip.
	probes := primary.healthHits.Load()
	if _, _, err := bridge.Analyze(ctx, service.AnalyzeRequest{Content: "y", Type: "text"}); err != nil {
		t.Fatalf("second Analyze: %v", err)
	}
	if got := primary.healthHits.Load(); got != probes {
		t.Errorf("health probes went %d → %d; a 401 must not invalidate the probe", probes, got)
	}
	if primary.analyzeHits.Load() != 2 {
		t.Errorf("primary analyze hits = %d; the walk must keep trying the live primary first", primary.analyzeHits.Load())
	}
}

// TestAuth_AnalyzeAuthRejectionFallsBackLocally: with every instance
// rejecting credentials the keyed request lands on the local fallback —
// replay-safe because the middleware rejected before enqueue.
func TestAuth_AnalyzeAuthRejectionFallsBackLocally(t *testing.T) {
	inst := newMockInstance(t, "job-1")
	inst.requireToken = "right-token"
	fb := &fakeAnalyzer{jobID: "job-local"}
	var warns bytes.Buffer
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:       []idxbridge.Endpoint{{URL: inst.url(), Token: "wrong-token"}},
		AnalyzeFallback: fb,
		WarnWriter:      &warns,
	})

	jobID, servedBy, err := bridge.Analyze(context.Background(), service.AnalyzeRequest{Content: "x", Type: "text"})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !fb.called || jobID != "job-local" || servedBy != "" {
		t.Errorf("jobID=%q servedBy=%q fb.called=%v; want the local fallback", jobID, servedBy, fb.called)
	}
	if !strings.Contains(warns.String(), inst.url()) {
		t.Errorf("warning %q must name the rejecting instance", warns.String())
	}
}

// TestAuth_SearchAuthRejectionWarnsOnFallback: a 401 on search walks on and,
// when the query ends up served by the local corpus, says so loudly — never
// a silent divert.
func TestAuth_SearchAuthRejectionWarnsOnFallback(t *testing.T) {
	inst := newMockInstance(t, "s")
	inst.requireToken = "right-token"
	fb := &fakeFallback{total: 3}
	var warns bytes.Buffer
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints:  []idxbridge.Endpoint{{URL: inst.url(), Token: "wrong-token"}},
		Fallback:   fb,
		WarnWriter: &warns,
	})

	_, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if !fb.called || total != 3 {
		t.Fatalf("fallback not used (called=%v total=%d)", fb.called, total)
	}
	w := warns.String()
	if !strings.Contains(w, inst.url()) || !strings.Contains(w, "401") {
		t.Errorf("warning %q must name the rejecting instance and status", w)
	}
	if !strings.Contains(w, "local") {
		t.Errorf("warning %q must say results come from the local corpus", w)
	}
}

// TestAuth_Search401DoesNotSurface: unlike other 4xx, a 401 is not a
// query-level rejection — it must not surface as the search error while a
// fallback exists.
func TestAuth_Search401DoesNotSurface(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.requireToken = "right-token"
	secondary := newMockInstance(t, "s")
	fb := &fakeFallback{}
	var warns bytes.Buffer
	bridge := idxbridge.New(idxbridge.Config{
		Endpoints: []idxbridge.Endpoint{
			{URL: primary.url()},
			{URL: secondary.url()},
		},
		Fallback:   fb,
		WarnWriter: &warns,
	})

	objs, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v; a 401 must walk, not surface", err)
	}
	if total != 1 || len(objs) != 1 || objs[0].ID != "obj-s" {
		t.Errorf("got %d objs (total %d); want the next instance's result", len(objs), total)
	}
	if fb.called {
		t.Error("local fallback used while the next instance was live")
	}
}
