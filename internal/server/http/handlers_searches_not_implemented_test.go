package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// noSearchStateDriver is a storage backend whose saved-search and
// search-history stores are the Postgres placeholders: every method
// answers storage.ErrNotImplemented.
type noSearchStateDriver struct{ storage.StorageDriver }

func (noSearchStateDriver) SavedSearches() storage.SavedSearchStore {
	return &postgres.SavedSearchStore{}
}
func (noSearchStateDriver) SearchHistory() storage.SearchHistoryStore {
	return &postgres.SearchHistoryStore{}
}

// A backend without a saved-search or search-history store answers 501
// NOT_IMPLEMENTED on every route over them, as the alias routes do: the
// gap is the backend's, not a server failure, and nothing is returned.
func TestSearchState_NoStore_NotImplemented(t *testing.T) {
	driver := noSearchStateDriver{storageutil.NewTestDriver(t)}
	svc := service.New(driver, jobs.NewQueue(driver.Jobs()), builtins.Registry(), search.NewEngine(driver), "", nil)
	srv := httptest.NewServer(NewRouter(svc, false, nil))
	defer srv.Close()

	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/saved-searches", map[string]string{"name": "s", "query": "type==note"}},
		{http.MethodGet, "/api/v1/saved-searches", nil},
		{http.MethodGet, "/api/v1/saved-searches/s", nil},
		{http.MethodDelete, "/api/v1/saved-searches/s", nil},
		{http.MethodGet, "/api/v1/search-history", nil},
		{http.MethodDelete, "/api/v1/search-history?profile_id=work", nil},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			var body *bytes.Reader
			if tc.body != nil {
				b, err := json.Marshal(tc.body)
				require.NoError(t, err)
				body = bytes.NewReader(b)
			} else {
				body = bytes.NewReader(nil)
			}
			req, err := http.NewRequest(tc.method, srv.URL+tc.path, body)
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
			var env ErrorEnvelope
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&env))
			assert.Equal(t, "NOT_IMPLEMENTED", env.Error.Code)
		})
	}
}
