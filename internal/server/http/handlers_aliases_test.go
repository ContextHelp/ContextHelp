package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedAliasObject stores one knowledge object an alias can point at.
func seedAliasObject(t *testing.T, ts *testServerBundle, id string) {
	t.Helper()
	now := time.Now().Truncate(time.Second)
	require.NoError(t, ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: id, Type: "note", CreatedAt: now, UpdatedAt: now,
	}))
}

func aliasRequest(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, url, rdr)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func aliasErrorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var env ErrorEnvelope
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
	return env.Error.Code
}

// TestAliases_RoundTrip: a created alias is stored, listed, resolved and
// deleted — the 201 is backed by a row, not an echo of the request.
func TestAliases_RoundTrip(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedAliasObject(t, ts, "obj-1")

	resp := aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "charter", "object_id": "obj-1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var created storage.Alias
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	assert.Equal(t, "charter", created.Alias)
	assert.Equal(t, "obj-1", created.ObjectID)
	assert.Equal(t, "global", created.Scope)

	// The row is in the store itself.
	id, err := ts.svc.Store.Aliases().Resolve(context.Background(), "charter", "")
	require.NoError(t, err)
	assert.Equal(t, "obj-1", id)

	resp = aliasRequest(t, http.MethodGet, ts.URL+"/api/v1/aliases?object_id=obj-1", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var listed []storage.Alias
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&listed))
	require.Len(t, listed, 1)
	assert.Equal(t, "charter", listed[0].Alias)

	resp = aliasRequest(t, http.MethodGet, ts.URL+"/api/v1/aliases/charter", nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var resolved map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&resolved))
	assert.Equal(t, "obj-1", resolved["object_id"])

	resp = aliasRequest(t, http.MethodDelete, ts.URL+"/api/v1/aliases/charter", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = aliasRequest(t, http.MethodGet, ts.URL+"/api/v1/aliases/charter", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Deleting an alias that does not exist is a 404, not a silent 204.
	resp = aliasRequest(t, http.MethodDelete, ts.URL+"/api/v1/aliases/charter", nil)
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCreateAlias_UnknownObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp := aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "ghost", "object_id": "no-such-object"})
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	assert.Equal(t, "NOT_FOUND", aliasErrorCode(t, resp))
}

func TestCreateAlias_Duplicate(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedAliasObject(t, ts, "obj-1")
	seedAliasObject(t, ts, "obj-2")

	resp := aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "charter", "object_id": "obj-1"})
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	resp = aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "charter", "object_id": "obj-2"})
	assert.Equal(t, http.StatusConflict, resp.StatusCode)
	assert.Equal(t, "CONFLICT", aliasErrorCode(t, resp))

	id, err := ts.svc.Store.Aliases().Resolve(context.Background(), "charter", "")
	require.NoError(t, err)
	assert.Equal(t, "obj-1", id, "the first alias must survive the rejected duplicate")
}

func TestCreateAlias_Validation(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedAliasObject(t, ts, "obj-1")

	cases := map[string]map[string]string{
		"missing alias":          {"object_id": "obj-1"},
		"missing object_id":      {"alias": "a"},
		"unknown scope":          {"alias": "a", "object_id": "obj-1", "scope": "owner"},
		"profile scope, no name": {"alias": "a", "object_id": "obj-1", "scope": "profile"},
		"global with a profile":  {"alias": "a", "object_id": "obj-1", "scope": "global", "profile": "work"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases", body)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
	all, err := ts.svc.Store.Aliases().List(context.Background(), storage.AliasFilter{})
	require.NoError(t, err)
	assert.Empty(t, all)
}

// TestAliases_ProfileScope: a profile alias resolves only for its profile
// and shadows a global alias of the same name.
func TestAliases_ProfileScope(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	seedAliasObject(t, ts, "obj-g")
	seedAliasObject(t, ts, "obj-p")

	require.Equal(t, http.StatusCreated, aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "plan", "object_id": "obj-g"}).StatusCode)
	require.Equal(t, http.StatusCreated, aliasRequest(t, http.MethodPost, ts.URL+"/api/v1/aliases",
		map[string]string{"alias": "plan", "object_id": "obj-p", "scope": "profile", "profile": "work"}).StatusCode)

	resolve := func(q string) string {
		resp := aliasRequest(t, http.MethodGet, ts.URL+"/api/v1/aliases/plan"+q, nil)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var out map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		return out["object_id"]
	}
	assert.Equal(t, "obj-p", resolve("?profile=work"))
	assert.Equal(t, "obj-g", resolve(""))

	resp := aliasRequest(t, http.MethodDelete, ts.URL+"/api/v1/aliases/plan?scope=profile&profile=work", nil)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "obj-g", resolve("?profile=work"))
}

// noAliasDriver is a storage backend without an alias store.
type noAliasDriver struct{ storage.StorageDriver }

func (noAliasDriver) Aliases() storage.AliasStore { return nil }

// TestAliases_NoStore_NotImplemented: a backend without an alias store
// answers 501 on every alias route instead of claiming success.
func TestAliases_NoStore_NotImplemented(t *testing.T) {
	driver := noAliasDriver{storageutil.NewTestDriver(t)}
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	srv := httptest.NewServer(NewRouter(svc, false, nil))
	defer srv.Close()

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/aliases", map[string]string{"alias": "a", "object_id": "obj-1"}},
		{http.MethodGet, "/api/v1/aliases", nil},
		{http.MethodGet, "/api/v1/aliases/a", nil},
		{http.MethodDelete, "/api/v1/aliases/a", nil},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resp := aliasRequest(t, tc.method, srv.URL+tc.path, tc.body)
			assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
			assert.Equal(t, "NOT_IMPLEMENTED", aliasErrorCode(t, resp))
		})
	}
}
