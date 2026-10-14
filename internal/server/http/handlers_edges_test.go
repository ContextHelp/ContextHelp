package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// edgeServer is a protected router with one static token per role over a
// store holding objects a, b and c.
func edgeServer(t *testing.T) (*httptest.Server, storage.StorageDriver) {
	t.Helper()
	driver := storageutil.NewTestDriver(t)
	now := time.Now().Truncate(time.Second)
	for _, id := range []string{"a", "b", "c"} {
		require.NoError(t, driver.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: id, Type: "text", CreatedAt: now, UpdatedAt: now,
		}))
	}
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	provider, err := authn.NewStatic([]authn.StaticToken{
		{Token: "tok-admin", Principal: "owner", Roles: []string{authn.RoleAdmin}},
		{Token: "tok-writer", Principal: "phone", Roles: []string{authn.RoleWriter}},
		{Token: "tok-reader", Principal: "tablet", Roles: []string{authn.RoleReader}},
	})
	require.NoError(t, err)
	ts := httptest.NewServer(NewRouterWithConfig(svc, RouterConfig{Auth: provider}))
	t.Cleanup(ts.Close)
	return ts, driver
}

// storedLinks lists the object-to-object edges leaving a, b and c as
// "from type to", sorted.
func storedLinks(t *testing.T, driver storage.StorageDriver) []string {
	t.Helper()
	var out []string
	for _, id := range []string{"a", "b", "c"} {
		edges, err := driver.Edges().ListFrom(context.Background(), "object", id)
		require.NoError(t, err)
		for _, e := range edges {
			out = append(out, e.FromID+" "+e.EdgeType+" "+e.ToID)
		}
	}
	slices.Sort(out)
	return out
}

type edgeHops struct {
	Data []service.LinkHop `json:"data"`
}

func TestEdgesRoundTrip(t *testing.T) {
	ts, driver := edgeServer(t)

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/edges", "tok-writer",
		map[string]string{"source": "a", "target": "b", "type": "extends", "context": "why"})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
	var created struct {
		Data []storage.Edge `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.Len(t, created.Data, 2)
	assert.Equal(t, "extends", created.Data[0].EdgeType)
	assert.Equal(t, "extended-by", created.Data[1].EdgeType)
	assert.Equal(t, "why", created.Data[1].Metadata["context"])
	assert.Equal(t, []string{"a extends b", "b extended-by a"}, storedLinks(t, driver))

	resp, body = roleDo(t, http.MethodPost, ts.URL+"/api/v1/edges", "tok-writer",
		map[string]string{"source": "b", "target": "c", "type": "supports"})
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

	list := func(query string) []string {
		t.Helper()
		resp, body := roleDo(t, http.MethodGet, ts.URL+"/api/v1/objects/b/edges"+query, "tok-reader", nil)
		require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
		assert.Contains(t, string(body), `"data":[`, "an empty listing is [], not null")
		var got edgeHops
		require.NoError(t, json.Unmarshal(body, &got))
		out := []string{}
		for _, h := range got.Data {
			out = append(out, h.Direction+" "+h.Edge.EdgeType+" "+h.ObjectID)
		}
		return out
	}
	assert.Equal(t, []string{"out extended-by a", "out supports c", "in extends a", "in supported-by c"}, list(""))
	assert.Equal(t, []string{"in extends a", "in supported-by c"}, list("?direction=in"))
	assert.Equal(t, []string{"out supports c"}, list("?type=supports"))
	assert.Equal(t, []string{"out extended-by a", "out supports c"}, list("?depth=3"))

	resp, body = roleDo(t, http.MethodDelete, ts.URL+"/api/v1/edges?source=b&target=a&type=extended-by", "tok-admin", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"deleted":2}`, string(body))
	assert.Equal(t, []string{"b supports c", "c supported-by b"}, storedLinks(t, driver))

	resp, body = roleDo(t, http.MethodDelete, ts.URL+"/api/v1/edges?source=c&target=b", "tok-admin", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.JSONEq(t, `{"deleted":2}`, string(body))
	assert.Empty(t, storedLinks(t, driver))
	assert.Equal(t, []string{}, list(""))
}

// Create takes write:objects, the listing read:objects, delete the
// admin-only delete:edges: a writer links but cannot unlink.
func TestEdgesRoles(t *testing.T) {
	ts, driver := edgeServer(t)
	link := map[string]string{"source": "a", "target": "b", "type": "extends"}
	del := ts.URL + "/api/v1/edges?source=a&target=b"
	get := ts.URL + "/api/v1/objects/a/edges"

	resp, body := roleDo(t, http.MethodPost, ts.URL+"/api/v1/edges", "tok-reader", link)
	assertScopeDenied(t, resp, body, authn.ScopeWriteObjects)
	resp, body = roleDo(t, http.MethodPost, ts.URL+"/api/v1/edges", "tok-writer", link)
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))

	resp, body = roleDo(t, http.MethodDelete, del, "tok-writer", nil)
	assertScopeDenied(t, resp, body, authn.ScopeDeleteEdges)
	resp, body = roleDo(t, http.MethodDelete, del, "tok-reader", nil)
	assertScopeDenied(t, resp, body, authn.ScopeDeleteEdges)
	assert.Len(t, storedLinks(t, driver), 2, "a refused delete removed edges")

	for _, r := range []struct{ method, url string }{
		{http.MethodPost, ts.URL + "/api/v1/edges"}, {http.MethodGet, get}, {http.MethodDelete, del},
	} {
		resp, _ := roleDo(t, r.method, r.url, "", link)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "%s %s without a token", r.method, r.url)
	}

	resp, body = roleDo(t, http.MethodGet, get, "tok-reader", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	resp, body = roleDo(t, http.MethodDelete, del, "tok-admin", nil)
	assert.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	assert.Empty(t, storedLinks(t, driver))
}

func TestEdgesErrors(t *testing.T) {
	ts, driver := edgeServer(t)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   any
		status int
		code   string
	}{
		{"unknown type", http.MethodPost, "/api/v1/edges", map[string]string{"source": "a", "target": "b", "type": "imaginary"}, 400, "INVALID_REQUEST"},
		{"no type", http.MethodPost, "/api/v1/edges", map[string]string{"source": "a", "target": "b"}, 400, "INVALID_REQUEST"},
		{"unknown field", http.MethodPost, "/api/v1/edges", map[string]string{"source": "a", "target": "b", "type": "extends", "weight": "2"}, 400, "INVALID_REQUEST"},
		{"missing source", http.MethodPost, "/api/v1/edges", map[string]string{"source": "x", "target": "b", "type": "extends"}, 404, "NOT_FOUND"},
		{"missing target", http.MethodPost, "/api/v1/edges", map[string]string{"source": "a", "target": "x", "type": "extends"}, 404, "NOT_FOUND"},
		{"list unknown object", http.MethodGet, "/api/v1/objects/x/edges", nil, 404, "NOT_FOUND"},
		{"list unknown param", http.MethodGet, "/api/v1/objects/a/edges?follow=2", nil, 400, CodeInvalidParam},
		{"list bad direction", http.MethodGet, "/api/v1/objects/a/edges?direction=up", nil, 400, CodeInvalidParam},
		{"list depth 4", http.MethodGet, "/api/v1/objects/a/edges?depth=4", nil, 400, CodeInvalidParam},
		{"list depth 0", http.MethodGet, "/api/v1/objects/a/edges?depth=0", nil, 400, CodeInvalidParam},
		{"delete no target", http.MethodDelete, "/api/v1/edges?source=a", nil, 400, CodeInvalidParam},
		{"delete unknown param", http.MethodDelete, "/api/v1/edges?source=a&target=b&id=e", nil, 400, CodeInvalidParam},
		{"delete unknown type", http.MethodDelete, "/api/v1/edges?source=a&target=b&type=imaginary", nil, 400, CodeInvalidParam},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := roleDo(t, tc.method, ts.URL+tc.path, "tok-admin", tc.body)
			assert.Equal(t, tc.status, resp.StatusCode, string(body))
			assert.Equal(t, tc.code, errorCode(t, body))
		})
	}
	assert.Empty(t, storedLinks(t, driver), "a refused request stored edges")
}
