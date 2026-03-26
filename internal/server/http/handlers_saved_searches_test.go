package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSavedSearch(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{
		"name":  "my-search",
		"query": "type == 'note'",
	})
	resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var ss storage.SavedSearch
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ss))
	assert.Equal(t, "my-search", ss.Name)
	assert.Equal(t, "type == 'note'", ss.Query)
	assert.NotEmpty(t, ss.ID)
}

func TestCreateSavedSearch_MissingName(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"query": "q"})
	resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestCreateSavedSearch_MissingQuery(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"name": "n"})
	resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListSavedSearches(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	for _, name := range []string{"s1", "s2"} {
		body, _ := json.Marshal(map[string]string{"name": name, "query": "q"})
		resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	}

	resp, err := http.Get(ts.URL + "/api/v1/saved-searches")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data []*storage.SavedSearch `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Data, 2)
}

func TestGetSavedSearch(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"name": "get-me", "query": "q"})
	resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/saved-searches/get-me")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var ss storage.SavedSearch
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&ss))
	assert.Equal(t, "get-me", ss.Name)
}

func TestGetSavedSearch_NotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/saved-searches/does-not-exist")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestDeleteSavedSearch(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	body, _ := json.Marshal(map[string]string{"name": "del-me", "query": "q"})
	resp, err := http.Post(ts.URL+"/api/v1/saved-searches", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/saved-searches/del-me", nil)
	resp, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp, err = http.Get(ts.URL + "/api/v1/saved-searches/del-me")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}
