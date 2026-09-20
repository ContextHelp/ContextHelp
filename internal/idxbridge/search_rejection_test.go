package idxbridge_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

// searchBridge builds a two-instance bridge with a capturable local fallback.
func searchBridge(t *testing.T, primary, secondary *mockInstance, fb *fakeFallback) *idxbridge.IdxBridge {
	t.Helper()
	return idxbridge.New(idxbridge.Config{
		BaseURLs:              []string{primary.url(), secondary.url()},
		ProbeTimeout:          200 * time.Millisecond,
		NegativeProbeInterval: 20 * time.Millisecond,
		Fallback:              fb,
	})
}

// TestSearch_400SurfacesWithDiagnostic: a completed HTTP exchange with a 4xx
// is a live instance rejecting the query — the error surfaces with the
// instance's own diagnostic, and neither the next instance nor the local
// fallback re-runs the rejected query.
func TestSearch_400SurfacesWithDiagnostic(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.searchStatus = http.StatusBadRequest
	secondary := newMockInstance(t, "s")
	fb := &fakeFallback{}
	bridge := searchBridge(t, primary, secondary, fb)

	_, _, err := bridge.SearchObjects(context.Background(), "type=~broken", 5, 0)
	if err == nil {
		t.Fatal("SearchObjects succeeded; want the live instance's 400 surfaced")
	}
	var rerr *idxbridge.RemoteError
	if !errors.As(err, &rerr) || rerr.StatusCode != http.StatusBadRequest {
		t.Fatalf("error = %v; want *RemoteError 400", err)
	}
	if !strings.Contains(rerr.Body, "BAD_QUERY") {
		t.Errorf("Body = %q; want the instance's diagnostic preserved", rerr.Body)
	}
	if secondary.searchHits.Load() != 0 {
		t.Error("rejected query replayed against the next instance")
	}
	if fb.called {
		t.Error("rejected query replayed against the local fallback")
	}
}

// TestSearch_NoProbeInvalidationOn4xx: a 4xx comes from a live instance — the
// cached up-probe must survive, so the next search goes straight back to the
// instance without a fresh /health round-trip.
func TestSearch_NoProbeInvalidationOn4xx(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.searchStatus = http.StatusBadRequest
	secondary := newMockInstance(t, "s")
	fb := &fakeFallback{}
	bridge := searchBridge(t, primary, secondary, fb)

	ctx := context.Background()
	if _, _, err := bridge.SearchObjects(ctx, "type=~broken", 5, 0); err == nil {
		t.Fatal("first search: want 400 surfaced")
	}
	probes := primary.healthHits.Load()

	if _, _, err := bridge.SearchObjects(ctx, "type=~broken", 5, 0); err == nil {
		t.Fatal("second search: want 400 surfaced")
	}
	if got := primary.healthHits.Load(); got != probes {
		t.Errorf("health probes went %d → %d after a 4xx; a live rejection must not invalidate the probe", probes, got)
	}
	if primary.searchHits.Load() != 2 {
		t.Errorf("search hits = %d; the second query must go straight back to the live instance", primary.searchHits.Load())
	}
}

// TestSearch_5xxWalksToNext: a 5xx means the instance is failing — walk to
// the next instance and invalidate the probe so the next call re-checks.
func TestSearch_5xxWalksToNext(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.searchStatus = http.StatusInternalServerError
	secondary := newMockInstance(t, "s")
	fb := &fakeFallback{}
	bridge := searchBridge(t, primary, secondary, fb)

	ctx := context.Background()
	objs, total, err := bridge.SearchObjects(ctx, "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if total != 1 || len(objs) != 1 || objs[0].ID != "obj-s" {
		t.Errorf("got %d objs (total %d); want the next instance's result after a 5xx", len(objs), total)
	}
	if fb.called {
		t.Error("local fallback used while the next instance was live")
	}

	// The 5xx must have invalidated the probe: the next call re-probes.
	probes := primary.healthHits.Load()
	if _, _, err := bridge.SearchObjects(ctx, "type==note", 5, 0); err != nil {
		t.Fatalf("second SearchObjects: %v", err)
	}
	if got := primary.healthHits.Load(); got == probes {
		t.Error("no fresh health probe after a 5xx; a failing instance must be re-checked")
	}
}

// TestSearch_TransportFailureStillWalks: instances that die mid-exchange keep
// the original walk semantics.
func TestSearch_TransportFailureStillWalks(t *testing.T) {
	primary := newMockInstance(t, "p")
	primary.healthy.Store(false)
	secondary := newMockInstance(t, "s")
	fb := &fakeFallback{}
	bridge := searchBridge(t, primary, secondary, fb)

	objs, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if total != 1 || len(objs) != 1 || objs[0].ID != "obj-s" {
		t.Errorf("got %d objs (total %d); want the next instance's result", len(objs), total)
	}
}
