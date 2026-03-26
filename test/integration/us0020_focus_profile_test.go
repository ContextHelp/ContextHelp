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
// returns a 200 response and does not error when a profile parameter is present.
// Profile-scoped filtering via HTTP is exercised via the service layer directly
// (TestUS0020_FocusProfileRestrictsSearchScope); the HTTP handler currently
// applies the RSQL filter without profile scoping.
func TestUS0020_FocusProfileViaHTTPSearchQuery(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-http-1", Type: "article", ProfileID: "ops",
		CreatedAt: now, UpdatedAt: now,
	}))

	// HTTP search endpoint must respond 200 (no crash or 5xx) when
	// profile parameter is supplied.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==article&profile=ops")
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	// At least the one ingested object must be visible (global search, no
	// HTTP-level profile scoping in current implementation).
	assert.True(t, containsID(body.Data, "fp-http-1"),
		"ingested article must appear in search results")
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
