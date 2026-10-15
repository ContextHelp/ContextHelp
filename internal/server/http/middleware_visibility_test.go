package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// Every route declares one of the known shapes.
func TestEveryRouteDeclaresAShape(t *testing.T) {
	for _, s := range APIRouteSpecs() {
		shape, known := RouteShape(s.Method, s.Pattern)
		assert.True(t, known, "route %s %s has no shape", s.Method, s.Pattern)
		assert.True(t, slices.Contains(sharing.Shapes, shape), "route %s %s declares shape %q", s.Method, s.Pattern, shape)
	}
	// Owner-only and write routes carry nothing.
	for _, key := range []string{"GET /jobs", "POST /analyze", "DELETE /objects/{id}", "GET /whoami", "GET /share/audiences"} {
		assert.Equal(t, sharing.ShapeNone, routeAccessTable[key].shape, key)
	}
	for _, key := range []string{"GET /objects", "GET /objects/{id}", "POST /find", "GET /entities/{slug}/backlinks", "ANY /mcp"} {
		assert.Equal(t, sharing.ShapeObjects, routeAccessTable[key].shape, key)
	}
	assert.Equal(t, sharing.ShapeEntities, routeAccessTable["GET /entities"].shape)
	assert.Equal(t, sharing.ShapeMixed, routeAccessTable["GET /search/graph"].shape)
}

// restrictedViewer compiles a colleague viewer for the middleware tests.
func restrictedViewer(t *testing.T) sharing.Viewer {
	t.Helper()
	tpl, _ := sharing.Template(sharing.TemplateColleagues)
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_vis", Grants: []sharing.Grant{{ID: "g", Audience: "a", Policy: tpl}}})
	require.NoError(t, err)
	return v
}

// serveVisibility runs handler behind Visibility on GET /objects as
// viewer and returns the recorded answer.
func serveVisibility(t *testing.T, v sharing.Viewer, strict bool, pattern string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	h := Visibility(http.MethodGet, pattern, VisibilityConfig{Strict: strict})(handler)
	req := httptest.NewRequestWithContext(sharing.WithViewer(context.Background(), v), http.MethodGet, "/api/v1"+pattern, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A body naming an object the ledger does not hold is stripped, or
// refused on a strict instance; a body of released objects passes
// intact; the owner is never touched.
func TestVisibilityStripsOrRefusesUnreleasedIDs(t *testing.T) {
	v := restrictedViewer(t)
	leak := func(w http.ResponseWriter, r *http.Request) {
		sharing.LedgerFrom(r.Context()).AddObject("a")
		WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "a"}, {"id": "hidden"}}, "total": 2})
	}
	rec := serveVisibility(t, v, false, "/objects", leak)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[{"id":"a"}],"total":2}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	rec = serveVisibility(t, v, true, "/objects", leak)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeVisibilityBackstop)
	assert.NotContains(t, rec.Body.String(), "hidden")

	clean := func(w http.ResponseWriter, r *http.Request) {
		sharing.LedgerFrom(r.Context()).AddObject("a")
		WriteJSON(w, http.StatusOK, map[string]any{"data": []map[string]any{{"id": "a", "raw_content": "fine"}}, "total": 1})
	}
	rec = serveVisibility(t, v, true, "/objects", clean)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":[{"id":"a","raw_content":"fine"}],"total":1}`, rec.Body.String())

	rec = serveVisibility(t, sharing.Unrestricted, true, "/objects", leak)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hidden", "the owner's answers are never checked")
}

// A withheld value found in the body is replaced by its category
// token, or refused strictly; a root object outside the ledger is
// answered absent.
func TestVisibilityCanaryAndAbsent(t *testing.T) {
	v := restrictedViewer(t)
	canary := func(w http.ResponseWriter, r *http.Request) {
		l := sharing.LedgerFrom(r.Context())
		l.AddObject("a")
		l.AddWithheld([]sharing.Withheld{{Category: "email", Value: "sarah.lee@acme.example"}})
		WriteJSON(w, http.StatusOK, map[string]any{"id": "a", "raw_content": "mail sarah.lee@acme.example today"})
	}
	rec := serveVisibility(t, v, false, "/objects/{id}", canary)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"a","raw_content":"mail [email] today"}`, rec.Body.String())
	rec = serveVisibility(t, v, true, "/objects/{id}", canary)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	absent := func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"id": "hidden", "raw_content": "x"})
	}
	rec = serveVisibility(t, v, false, "/objects/{id}", absent)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), CodeNotFound)
	assert.NotContains(t, rec.Body.String(), "hidden")
}

// Error envelopes and shape-less routes pass as they are; a route
// with no declared shape fails closed for a restricted viewer and
// never for the owner.
func TestVisibilityPrecisionAndUnclassified(t *testing.T) {
	v := restrictedViewer(t)
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, CodeNotFound, "object not found")
	}
	rec := serveVisibility(t, v, true, "/objects/{id}", notFound)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"NOT_FOUND","message":"object not found","details":{}}}`, rec.Body.String())

	whoami := func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]any{"principal": "prn_vis", "shared": map[string]any{"policies": []string{"colleagues-network"}}})
	}
	rec = serveVisibility(t, v, true, "/whoami", whoami)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"principal":"prn_vis","shared":{"policies":["colleagues-network"]}}`, rec.Body.String())

	rec = serveVisibility(t, v, false, "/no-such-route", whoami)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	rec = serveVisibility(t, sharing.Unrestricted, false, "/no-such-route", whoami)
	assert.Equal(t, http.StatusOK, rec.Code)
}
