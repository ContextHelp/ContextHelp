package sqlite

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSavedSearchStore_CreateAndGet(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()

	require.NoError(t, db.Init(ctx))

	ss := &storage.SavedSearch{
		ID:        "ss_test01",
		Name:      "my-search",
		Query:     "type = 'note'",
		ProfileID: "prof_abc",
		AlertOn:   "new-results",
		Notify:    "email",
	}
	// zero-value times are fine for test; scanner handles RFC3339 format
	require.NoError(t, db.SavedSearches().Create(ctx, ss))

	got, err := db.SavedSearches().GetByName(ctx, "my-search")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, ss.ID, got.ID)
	assert.Equal(t, ss.Name, got.Name)
	assert.Equal(t, ss.Query, got.Query)
	assert.Equal(t, ss.ProfileID, got.ProfileID)
	assert.Equal(t, ss.AlertOn, got.AlertOn)
	assert.Equal(t, ss.Notify, got.Notify)
}

func TestSavedSearchStore_GetByName_NotFound(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	got, err := db.SavedSearches().GetByName(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSavedSearchStore_List(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	for i, name := range []string{"search-a", "search-b", "search-c"} {
		require.NoError(t, db.SavedSearches().Create(ctx, &storage.SavedSearch{
			ID:        "ss_" + name,
			Name:      name,
			Query:     "q" + string(rune('0'+i)),
			ProfileID: "prof_xyz",
		}))
	}

	all, err := db.SavedSearches().List(ctx, storage.SavedSearchFilter{ProfileID: "prof_xyz"})
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// limit
	limited, err := db.SavedSearches().List(ctx, storage.SavedSearchFilter{
		ProfileID: "prof_xyz", Limit: 2,
	})
	require.NoError(t, err)
	assert.Len(t, limited, 2)
}

func TestSavedSearchStore_Update(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	ss := &storage.SavedSearch{
		ID:    "ss_upd01",
		Name:  "updatable",
		Query: "original",
	}
	require.NoError(t, db.SavedSearches().Create(ctx, ss))

	ss.Query = "updated-query"
	require.NoError(t, db.SavedSearches().Update(ctx, ss))

	got, err := db.SavedSearches().GetByName(ctx, "updatable")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "updated-query", got.Query)
}

func TestSavedSearchStore_Delete(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	ss := &storage.SavedSearch{
		ID:    "ss_del01",
		Name:  "to-delete",
		Query: "q",
	}
	require.NoError(t, db.SavedSearches().Create(ctx, ss))

	require.NoError(t, db.SavedSearches().Delete(ctx, "to-delete"))

	got, err := db.SavedSearches().GetByName(ctx, "to-delete")
	require.NoError(t, err)
	assert.Nil(t, got)
}
