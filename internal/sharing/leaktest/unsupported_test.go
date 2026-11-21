package leaktest

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// noSearchStateDriver is SQLite with the Postgres saved-search and
// search-history placeholders, whose every method answers
// storage.ErrNotImplemented: a backend that lacks those stores.
type noSearchStateDriver struct{ storage.StorageDriver }

func (noSearchStateDriver) SavedSearches() storage.SavedSearchStore {
	return &postgres.SavedSearchStore{}
}
func (noSearchStateDriver) SearchHistory() storage.SearchHistoryStore {
	return &postgres.SearchHistoryStore{}
}

func newNoSearchStateDriver(t *testing.T) storage.StorageDriver {
	return noSearchStateDriver{storageutil.NewTestDriver(t)}
}

// On a backend without the saved-search and search-history stores the
// routes over them answer 501 NOT_IMPLEMENTED, which the harness takes
// as nothing returned: every case still runs clean for the
// administrators, and the owner-state test skips, naming why.
func TestAdminOnABackendWithoutSearchState(t *testing.T) {
	f := Start(t, newNoSearchStateDriver(t))
	for _, path := range []string{"/saved-searches", "/saved-searches/x", "/search-history"} {
		r := fetch(t, f, f.Owner(), path)
		assert.Equal(t, http.StatusNotImplemented, r.Status, "owner GET %s: %s", path, r.Body)
		assert.True(t, r.Unsupported(), path)
	}
	adminReadsEveryRouteSeesNothing(t, newNoSearchStateDriver)
	t.Run("owner state", func(t *testing.T) {
		adminSeesNoOneElsesSavedSearches(t, newNoSearchStateDriver)
		t.Error("the owner-state test ran on a backend without saved searches; it must skip")
	})
}
