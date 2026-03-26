package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchHistoryStore_AppendAndList(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	e := &storage.SearchHistoryEntry{
		ID:             "sh_test01",
		Query:          "find me notes",
		ProfileID:      "prof_abc",
		StrategiesUsed: "fts,vector",
		ResultCount:    5,
		SearchedAt:     time.Now().UTC().Truncate(time.Second),
	}
	require.NoError(t, db.SearchHistory().Append(ctx, e))

	results, err := db.SearchHistory().List(ctx, storage.SearchHistoryFilter{ProfileID: "prof_abc"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, e.ID, results[0].ID)
	assert.Equal(t, e.Query, results[0].Query)
	assert.Equal(t, e.StrategiesUsed, results[0].StrategiesUsed)
	assert.Equal(t, e.ResultCount, results[0].ResultCount)
}

func TestSearchHistoryStore_List_Pagination(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	for i := 0; i < 5; i++ {
		require.NoError(t, db.SearchHistory().Append(ctx, &storage.SearchHistoryEntry{
			ID:         "sh_page_" + string(rune('a'+i)),
			Query:      "q",
			ProfileID:  "prof_page",
			SearchedAt: time.Now().UTC().Truncate(time.Second),
		}))
	}

	page1, err := db.SearchHistory().List(ctx, storage.SearchHistoryFilter{
		ProfileID: "prof_page", Limit: 3,
	})
	require.NoError(t, err)
	assert.Len(t, page1, 3)

	all, err := db.SearchHistory().List(ctx, storage.SearchHistoryFilter{ProfileID: "prof_page"})
	require.NoError(t, err)
	assert.Len(t, all, 5)
}

func TestSearchHistoryStore_ClearByProfile(t *testing.T) {
	db := newTestDriver(t)
	ctx := context.Background()
	require.NoError(t, db.Init(ctx))

	for _, pid := range []string{"prof_a", "prof_b"} {
		require.NoError(t, db.SearchHistory().Append(ctx, &storage.SearchHistoryEntry{
			ID:         "sh_clr_" + pid,
			Query:      "q",
			ProfileID:  pid,
			SearchedAt: time.Now().UTC().Truncate(time.Second),
		}))
	}

	require.NoError(t, db.SearchHistory().ClearByProfile(ctx, "prof_a"))

	remaining, err := db.SearchHistory().List(ctx, storage.SearchHistoryFilter{})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "prof_b", remaining[0].ProfileID)
}
