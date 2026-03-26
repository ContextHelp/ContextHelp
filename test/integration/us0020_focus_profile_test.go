package integration

// US-0020: Apply Focus Profile to Search
// Configures a focus profile with a namespace (profile_id) filter and verifies
// that search is restricted to the profile's scope.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0020_FocusProfileRestrictsSearchScope verifies that passing a profileID
// to SearchObjects restricts results to objects owned by that profile.
func TestUS0020_FocusProfileRestrictsSearchScope(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	// Two objects in "engineering" profile, one in "security" profile.
	for _, tc := range []struct {
		id, typ, profile string
	}{
		{"fp-eng-1", "decision", "engineering"},
		{"fp-eng-2", "note", "engineering"},
		{"fp-sec-1", "decision", "security"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			ProfileID: tc.profile,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Search scoped to "engineering" profile.
	objs, total, err := env.svc.SearchObjects(ctx, "type==decision", 20, 0, "engineering")
	require.NoError(t, err)
	assert.Equal(t, 1, total, "only one decision belongs to engineering profile")
	require.Len(t, objs, 1)
	assert.Equal(t, "fp-eng-1", objs[0].ID)
}

// TestUS0020_FocusProfileAllObjectsWhenNoProfile verifies that omitting profileID
// returns all matching objects regardless of profile ownership.
func TestUS0020_FocusProfileAllObjectsWhenNoProfile(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct {
		id, profile string
	}{
		{"fp-all-1", "alpha"},
		{"fp-all-2", "beta"},
		{"fp-all-3", ""},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      "text",
			ProfileID: tc.profile,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// No profileID — global search (profile_id IS NULL or profile_id = '').
	objs, _, err := env.svc.SearchObjects(ctx, "type==text", 20, 0)
	require.NoError(t, err)
	// The global search must include the object without a profile_id.
	assert.True(t, containsIDPtr(objs, "fp-all-3"), "global search must include unscoped objects")
}

// TestUS0020_FocusProfileViaHTTPSearchQuery verifies the HTTP search endpoint
// parses the profile query param and scopes results to that profile.
func TestUS0020_FocusProfileViaHTTPSearchQuery(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-http-1", Type: "article", ProfileID: "ops",
		CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-http-2", Type: "article", ProfileID: "dev",
		CreatedAt: now, UpdatedAt: now,
	}))

	// HTTP search with profile=ops must only return "ops" objects.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==article&profile=ops")
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.True(t, containsID(body.Data, "fp-http-1"),
		"ops article must appear when profile=ops")
	assert.False(t, containsID(body.Data, "fp-http-2"),
		"dev article must not appear when profile=ops")
}

// TestUS0020_FocusProfileNoMatchReturnsEmpty verifies that a profile with no
// objects returns an empty list (not 404 or error).
func TestUS0020_FocusProfileNoMatchReturnsEmpty(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	objs, total, err := env.svc.SearchObjects(ctx, "type==text", 20, 0, "nonexistent-profile")
	require.NoError(t, err)
	assert.Equal(t, 0, total)
	assert.Empty(t, objs)
}
