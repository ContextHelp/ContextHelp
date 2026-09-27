package idxbridge_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
)

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
		Endpoints: []idxbridge.Endpoint{{URL: inst.url(), Token: "tok-3"}},
		Fallback:  &fakeFallback{},
	})

	if !bridge.Probe(context.Background()) {
		t.Fatal("Probe: instance should be reachable")
	}
	if got, _ := inst.healthAuth.Load().(string); got != "" {
		t.Errorf("health probe sent Authorization %q; probes must stay unauthenticated", got)
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
