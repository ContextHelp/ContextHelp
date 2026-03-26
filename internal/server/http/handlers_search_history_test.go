package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSearchHistory_Empty(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/search-history")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data []*storage.SearchHistoryEntry `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Data)
}

func TestListSearchHistory_WithEntries(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		require.NoError(t, ts.svc.AppendSearchHistory(ctx, "query", "prof_hist", "fts", i))
	}

	resp, err := http.Get(ts.URL + "/api/v1/search-history?profile_id=prof_hist")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var out struct {
		Data []*storage.SearchHistoryEntry `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Len(t, out.Data, 3)
}

func TestClearSearchHistory(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := context.Background()
	require.NoError(t, ts.svc.AppendSearchHistory(ctx, "q", "prof_clear", "fts", 1))

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/search-history?profile_id=prof_clear", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp, err = http.Get(ts.URL + "/api/v1/search-history?profile_id=prof_clear")
	require.NoError(t, err)
	defer resp.Body.Close()

	var out struct {
		Data []*storage.SearchHistoryEntry `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Data)
}

func TestClearSearchHistory_MissingProfileID(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/search-history", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
