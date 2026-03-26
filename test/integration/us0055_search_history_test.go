package integration

// US-0055: Search History and Recommendations
// Tests the closest available primitives: ResurfacingQueue as history-driven
// recommendations, and verifies that repeated RSQL queries remain stable.
//
// Note: A dedicated search-history table is not yet implemented. These tests
// validate the constituent capabilities (resurfacing queue, repeated search
// consistency) that underpin the story.

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0055_ResurfacingQueueActsAsRecommendation verifies that objects can be
// enqueued into the resurfacing queue (recommendation store) and listed back.
func TestUS0055_ResurfacingQueueActsAsRecommendation(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	// Create an object to resurface.
	obj := &storage.KnowledgeObject{
		ID:        "hist-resurf-01",
		Type:      "text",
		Summaries: []string{"machine learning feature engineering pipeline"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))

	// Enqueue as a resurfacing candidate (simulating "recommendation from history").
	entry := &storage.ResurfacingEntry{
		ID:        "entry-hist-01",
		ObjectID:  obj.ID,
		ProfileID: "default",
		Score:     0.85,
		Reason:    "frequently searched topic",
		CreatedAt: now,
	}
	require.NoError(t, env.svc.Store.Resurfacing().Upsert(ctx, entry))

	// List resurfacing queue — must include the entry.
	entries, err := env.svc.Store.Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID:  "default",
		UnseenOnly: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, entries, "resurfacing queue must contain the upserted entry")
	assert.Equal(t, "hist-resurf-01", entries[0].ObjectID)
}

// TestUS0055_RepeatedSearchReturnsConsistentResults verifies that running the
// same RSQL query multiple times returns identical result sets (deterministic).
func TestUS0055_RepeatedSearchReturnsConsistentResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"hist-rep-1", "decision"},
		{"hist-rep-2", "decision"},
		{"hist-rep-3", "note"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Run the same query three times.
	const query = "type==decision"
	var firstIDs []string
	for run := 0; run < 3; run++ {
		objs, _, err := env.svc.SearchObjects(ctx, query, 20, 0)
		require.NoError(t, err)
		ids := make([]string, 0, len(objs))
		for _, o := range objs {
			ids = append(ids, o.ID)
		}
		if run == 0 {
			firstIDs = ids
		} else {
			assert.Equal(t, firstIDs, ids, "run %d: same query must return same result set", run)
		}
	}
}

// TestUS0055_DismissingResurfacingEntryHidesItFromUnseenList verifies that
// dismissed entries no longer appear in the unseen resurfacing list.
func TestUS0055_DismissingResurfacingEntryHidesItFromUnseenList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	obj := &storage.KnowledgeObject{
		ID:        "hist-dismiss-01",
		Type:      "text",
		CreatedAt: now,
		UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Objects().Create(ctx, obj))

	entry := &storage.ResurfacingEntry{
		ID:        "entry-dismiss-01",
		ObjectID:  obj.ID,
		ProfileID: "default",
		Score:     0.7,
		Reason:    "test recommendation",
		CreatedAt: now,
	}
	require.NoError(t, env.svc.Store.Resurfacing().Upsert(ctx, entry))

	// Confirm it appears as unseen.
	before, err := env.svc.Store.Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID:  "default",
		UnseenOnly: true,
	})
	require.NoError(t, err)
	require.NotEmpty(t, before)

	// Dismiss the entry.
	require.NoError(t, env.svc.Store.Resurfacing().Dismiss(ctx, entry.ID, time.Now()))

	// Must no longer appear in unseen list.
	after, err := env.svc.Store.Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID:  "default",
		UnseenOnly: true,
	})
	require.NoError(t, err)
	for _, e := range after {
		assert.NotEqual(t, entry.ID, e.ID, "dismissed entry must not appear in unseen list")
	}
}

// TestUS0055_EmptyResurfacingQueueReturnsNoError verifies that listing an
// empty queue returns an empty slice (not an error).
func TestUS0055_EmptyResurfacingQueueReturnsNoError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	entries, err := env.svc.Store.Resurfacing().List(ctx, storage.ResurfacingFilter{
		ProfileID:  "nonexistent",
		UnseenOnly: true,
	})
	require.NoError(t, err)
	assert.Empty(t, entries)
}
