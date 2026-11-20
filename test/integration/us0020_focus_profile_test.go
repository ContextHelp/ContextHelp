package integration

// US-0020: Apply Focus Profile to Search
// Configures a focus profile with a namespace (profile_id) filter and verifies
// that search is restricted to the profile's scope.

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0020_FocusProfileDoesNotRestrictSearchScope verifies that naming a
// profile on a search returns every profile's matches: the profile is a lens.
func TestUS0020_FocusProfileDoesNotRestrictSearchScope(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	// Two objects in "engineering" profile, one in "security" profile.
	for _, tc := range []struct {
		id, typ, profile string
	}{
		{"fp-eng-1", "decision", "engineering"},
		{"fp-eng-2", "note", "engineering"},
		{"fp-sec-1", "decision", "security"},
	} {
		require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			ProfileID: tc.profile,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Search naming the "engineering" profile still sees both decisions.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==decision&profile=engineering")
	defer resp.Body.Close()
	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.Equal(t, 2, body.Total, "both decisions match whatever profile is named")
	assert.True(t, containsID(body.Data, "fp-eng-1"))
	assert.True(t, containsID(body.Data, "fp-sec-1"))
}

// TestUS0020_FocusProfileAllObjectsWhenNoProfile verifies that omitting profileID
// returns all matching objects regardless of profile ownership.
func TestUS0020_FocusProfileAllObjectsWhenNoProfile(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	for _, tc := range []struct {
		id, profile string
	}{
		{"fp-all-1", "alpha"},
		{"fp-all-2", "beta"},
		{"fp-all-3", ""},
	} {
		require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
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
// accepts the profile query param as a lens that does not narrow results.
func TestUS0020_FocusProfileViaHTTPSearchQuery(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-http-1", Type: "article", ProfileID: "ops",
		CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-http-2", Type: "article", ProfileID: "dev",
		CreatedAt: now, UpdatedAt: now,
	}))

	// HTTP search with profile=ops returns the "dev" object too.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==article&profile=ops")
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.True(t, containsID(body.Data, "fp-http-1"),
		"ops article must appear when profile=ops")
	assert.True(t, containsID(body.Data, "fp-http-2"),
		"dev article must still appear when profile=ops")
}

// TestUS0020_FocusProfileWithNoObjectsDoesNotEmptyResults verifies that naming
// a profile that owns nothing leaves the results as they are.
func TestUS0020_FocusProfileNoMatchReturnsEmpty(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()
	require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "fp-none-1", Type: "text", ProfileID: "alpha", CreatedAt: now, UpdatedAt: now,
	}))
	resp := doGet(t, env.URL+"/api/v1/search?q=type==text&profile=nonexistent-profile")
	defer resp.Body.Close()
	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.Equal(t, 1, body.Total)
	assert.True(t, containsID(body.Data, "fp-none-1"))
}
