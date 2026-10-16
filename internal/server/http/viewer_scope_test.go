package http

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ownerCtx is the context the tests act under when they call the
// service or its store directly: the owner's, unrestricted. Requests
// through the router get their viewer from the router.
func ownerCtx() context.Context {
	return sharing.WithViewer(context.Background(), sharing.Unrestricted)
}

// seedScopeObject stores one object of the owner's and returns its ID.
func seedScopeObject(t *testing.T, ts *testServerBundle) string {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	obj := &storage.KnowledgeObject{
		ID: "scope-owner-note", Type: "note", Status: storage.ObjectStatusActive,
		RawContent: "scope canary body", ContentHash: "sha256:scope-owner-note",
		CreatedAt: now, UpdatedAt: now,
	}
	if err := ts.svc.Store(ownerCtx()).Objects().Create(ownerCtx(), obj); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return obj.ID
}

// routeHandler returns the handler the API table mounts for method and
// pattern.
func routeHandler(t *testing.T, ts *testServerBundle, method, pattern string) http.Handler {
	t.Helper()
	for _, rt := range apiRoutes(ts.svc, RouterConfig{}) {
		if rt.Method == method && rt.Pattern == pattern {
			return rt.Handler
		}
	}
	t.Fatalf("no route %s %s", method, pattern)
	return nil
}

// A handler invoked on a request that carries no viewer reads nothing:
// the service's scoped driver refuses, the response is an error and the
// owner's object never appears. The same request through the router,
// which attaches the viewer, answers with the object: the viewer is the
// whole difference.
func TestHandlersFailClosedWithoutViewer(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	id := seedScopeObject(t, ts)

	withID := func(r *http.Request) *http.Request {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", id)
		return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	}
	cases := []struct {
		method, pattern, path, body string
		prepare                     func(*http.Request) *http.Request
	}{
		{method: http.MethodGet, pattern: "/objects", path: "/api/v1/objects"},
		{method: http.MethodGet, pattern: "/objects/{id}", path: "/api/v1/objects/" + id, prepare: withID},
		{method: http.MethodGet, pattern: "/search", path: "/api/v1/search?q=type==note"},
		{method: http.MethodPost, pattern: "/find", path: "/api/v1/find", body: `{"query":"canary","mode":"fts"}`},
		{method: "", pattern: "/mcp/", path: "/api/v1/mcp/",
			body: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"search","arguments":{"query":"canary","mode":"fts"}}}`},
	}
	for _, c := range cases {
		t.Run(strings.TrimSpace(c.method+" "+c.pattern), func(t *testing.T) {
			method := c.method
			if method == "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, c.path, strings.NewReader(c.body))
			req.Header.Set("Content-Type", "application/json")
			if c.prepare != nil {
				req = c.prepare(req)
			}
			rec := httptest.NewRecorder()
			routeHandler(t, ts, c.method, c.pattern).ServeHTTP(rec, req)
			body := rec.Body.String()
			if c.pattern == "/mcp/" {
				if !strings.Contains(body, `"error"`) {
					t.Fatalf("MCP without viewer answered without an error: %s", body)
				}
			} else if rec.Code < 400 {
				t.Fatalf("status %d without viewer, want an error: %s", rec.Code, body)
			}
			if strings.Contains(body, id) || strings.Contains(body, "scope canary body") {
				t.Fatalf("the owner's object leaked through a viewerless request: %s", body)
			}

			// Through the router, with its viewer, the same request answers.
			var resp *http.Response
			var err error
			if method == http.MethodGet {
				resp, err = http.Get(ts.URL + c.path)
			} else {
				resp, err = http.Post(ts.URL+c.path, "application/json", strings.NewReader(c.body))
			}
			if err != nil {
				t.Fatalf("router request: %v", err)
			}
			defer resp.Body.Close()
			routed, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(routed), id) {
				t.Fatalf("router: status %d, body %s; want 200 naming %s", resp.StatusCode, routed, id)
			}
		})
	}
}
