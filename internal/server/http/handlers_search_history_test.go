package http

import (
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

	ctx := ownerCtx()
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

	ctx := ownerCtx()
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

// Without profile_id the caller's whole history is cleared, entries
// recorded with no profile (ctxt find without --profile) included.
func TestClearSearchHistory_AllProfiles(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	ctx := ownerCtx()
	require.NoError(t, ts.svc.AppendSearchHistory(ctx, "q1", "", "fts", 1))
	require.NoError(t, ts.svc.AppendSearchHistory(ctx, "q2", "work", "fts", 1))

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/search-history", nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	left, err := ts.svc.ListSearchHistory(ctx, storage.SearchHistoryFilter{})
	require.NoError(t, err)
	assert.Empty(t, left)
}

// A search through /search is recorded and listable through /search-history.
func TestSearch_RecordsHistory(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(ts.URL + "/api/v1/search?q=type==article&profile=prof_rec")
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	resp, err := http.Get(ts.URL + "/api/v1/search-history?profile_id=prof_rec")
	require.NoError(t, err)
	defer resp.Body.Close()
	var out struct {
		Data []*storage.SearchHistoryEntry `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Len(t, out.Data, 2, "repeated identical queries are separate entries")
	assert.Equal(t, "type==article", out.Data[0].Query)
}

// search.history.enabled=false: GET /search answers and records nothing.
func TestSearch_HistoryDisabled(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()
	ts.svc.Cfg.Search.History.Enabled = false

	resp, err := http.Get(ts.URL + "/api/v1/search?q=type==article&profile=prof_off")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	resp, err = http.Get(ts.URL + "/api/v1/search-history?profile_id=prof_off")
	require.NoError(t, err)
	defer resp.Body.Close()
	var out struct {
		Data []*storage.SearchHistoryEntry `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	assert.Empty(t, out.Data)
}
