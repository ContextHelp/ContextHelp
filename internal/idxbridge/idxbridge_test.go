package idxbridge_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// fakeFallback is a minimal IdxSearcher for tests.
type fakeFallback struct {
	results []*storage.KnowledgeObject
	total   int
	err     error
	called  bool
}

func (f *fakeFallback) SearchObjects(
	_ context.Context, _ string, _, _ int, _ ...string,
) ([]*storage.KnowledgeObject, int, error) {
	f.called = true
	return f.results, f.total, f.err
}

// healthOnly serves just /health → 200.
func healthOnly() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}

// searchPayload marshals objects into the daemon response envelope.
func searchPayload(objs []*storage.KnowledgeObject, total int) []byte {
	b, _ := json.Marshal(map[string]any{
		"data":  objs,
		"total": total,
	})
	return b
}

func TestProbe_DaemonUp(t *testing.T) {
	srv := httptest.NewServer(healthOnly())
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:      srv.URL,
		ProbeTimeout: time.Second,
		Fallback:     fb,
		HTTPClient:   srv.Client(),
	})

	if !bridge.Probe(context.Background()) {
		t.Fatal("expected Probe to return true for live daemon")
	}
}

func TestProbe_DaemonDown(t *testing.T) {
	// No server started; just use a port no one is listening on.
	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:      "http://127.0.0.1:19999",
		ProbeTimeout: 50 * time.Millisecond,
		Fallback:     fb,
	})

	if bridge.Probe(context.Background()) {
		t.Fatal("expected Probe to return false for unreachable daemon")
	}
}

func TestProbe_Cached(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			callCount++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:       srv.URL,
		ProbeTimeout:  time.Second,
		ProbeInterval: time.Minute, // long cache
		Fallback:      fb,
		HTTPClient:    srv.Client(),
	})

	ctx := context.Background()
	bridge.Probe(ctx)
	bridge.Probe(ctx)
	bridge.Probe(ctx)

	if callCount != 1 {
		t.Errorf("health called %d times; expected 1 (cached)", callCount)
	}
}

func TestProbe_InvalidateResetsCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			callCount++
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:       srv.URL,
		ProbeTimeout:  time.Second,
		ProbeInterval: time.Minute,
		Fallback:      fb,
		HTTPClient:    srv.Client(),
	})

	ctx := context.Background()
	bridge.Probe(ctx)
	bridge.InvalidateProbe()
	bridge.Probe(ctx)

	if callCount != 2 {
		t.Errorf("health called %d times after invalidate; expected 2", callCount)
	}
}

func TestSearchObjects_ViaDaemon(t *testing.T) {
	objs := []*storage.KnowledgeObject{
		{ID: "obj-1", Type: "article"},
		{ID: "obj-2", Type: "note"},
	}
	var capturedQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/search":
			capturedQuery = r.URL.Query().Get("q")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(searchPayload(objs, 2))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{total: 99}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:    srv.URL,
		Fallback:   fb,
		HTTPClient: srv.Client(),
	})

	results, total, err := bridge.SearchObjects(context.Background(), "type==article", 10, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if total != 2 {
		t.Errorf("total: got %d, want 2", total)
	}
	if len(results) != 2 {
		t.Errorf("results len: got %d, want 2", len(results))
	}
	if capturedQuery != "type==article" {
		t.Errorf("query: got %q, want %q", capturedQuery, "type==article")
	}
	if fb.called {
		t.Error("fallback should not have been called when daemon is up")
	}
}

func TestSearchObjects_FallsBackWhenDaemonDown(t *testing.T) {
	expected := []*storage.KnowledgeObject{{ID: "fallback-obj"}}
	fb := &fakeFallback{results: expected, total: 1}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:      "http://127.0.0.1:19999",
		ProbeTimeout: 50 * time.Millisecond,
		Fallback:     fb,
	})

	results, total, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if !fb.called {
		t.Error("fallback should have been called when daemon is down")
	}
	if total != 1 {
		t.Errorf("total: got %d, want 1", total)
	}
	if len(results) != 1 || results[0].ID != "fallback-obj" {
		t.Errorf("unexpected results: %v", results)
	}
}

func TestSearchObjects_FallsBackOnDaemonSearchError(t *testing.T) {
	// Daemon health is up but /search returns 500 → fall back.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/search":
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{results: []*storage.KnowledgeObject{{ID: "fb-1"}}, total: 1}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:    srv.URL,
		Fallback:   fb,
		HTTPClient: srv.Client(),
	})

	results, _, err := bridge.SearchObjects(context.Background(), "type==note", 5, 0)
	if err != nil {
		t.Fatalf("SearchObjects: %v", err)
	}
	if !fb.called {
		t.Error("fallback should have been called after daemon search error")
	}
	if len(results) != 1 || results[0].ID != "fb-1" {
		t.Errorf("unexpected fallback results: %v", results)
	}
}

func TestSearchObjects_ProfilePropagated(t *testing.T) {
	var capturedProfile string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/search":
			capturedProfile = r.URL.Query().Get("profile")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(searchPayload(nil, 0))
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:    srv.URL,
		Fallback:   fb,
		HTTPClient: srv.Client(),
	})

	bridge.SearchObjects(context.Background(), "type==article", 10, 0, "growth")
	if capturedProfile != "growth" {
		t.Errorf("profile: got %q, want %q", capturedProfile, "growth")
	}
}

func TestNew_PanicsWithoutFallback(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when Fallback is nil")
		}
	}()
	idxbridge.New(idxbridge.Config{})
}

func TestSearchObjects_PaginationParams(t *testing.T) {
	var capturedLimit, capturedOffset string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/api/v1/search":
			capturedLimit = r.URL.Query().Get("limit")
			capturedOffset = r.URL.Query().Get("offset")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(searchPayload(nil, 0))
		}
	}))
	defer srv.Close()

	fb := &fakeFallback{}
	bridge := idxbridge.New(idxbridge.Config{
		BaseURL:    srv.URL,
		Fallback:   fb,
		HTTPClient: srv.Client(),
	})

	bridge.SearchObjects(context.Background(), "type==article", 15, 30)
	if capturedLimit != "15" {
		t.Errorf("limit: got %q, want %q", capturedLimit, "15")
	}
	if capturedOffset != "30" {
		t.Errorf("offset: got %q, want %q", capturedOffset, "30")
	}
}
