package integration

// US-0053: Cross-Profile Search Aggregation
// Configures multiple profiles, runs aggregated (global) search, verifies
// results from all profiles are present.

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0053_GlobalSearchIncludesAllProfiles verifies that searching without a
// profileID returns objects from every profile.
func TestUS0053_GlobalSearchIncludesAllProfiles(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	profiles := []string{"alpha", "beta", "gamma"}
	for i, p := range profiles {
		require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        "cp-obj-" + string(rune('a'+i)),
			Type:      "text",
			ProfileID: p,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}
	// Also add a global (no profile) object.
	require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "cp-global", Type: "text", CreatedAt: now, UpdatedAt: now,
	}))

	objs, _, err := env.svc.SearchObjects(ctx, "type==text", 50, 0)
	require.NoError(t, err)

	// Aggregated search spans every profile plus the unscoped object.
	for _, id := range []string{"cp-obj-a", "cp-obj-b", "cp-obj-c", "cp-global"} {
		assert.True(t, containsIDPtr(objs, id), "global search must include %s", id)
	}
}

// TestUS0053_PerProfileSearchSpansProfiles verifies that searches naming
// different profiles each return every profile's objects.
func TestUS0053_PerProfileSearchSpansProfiles(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	for _, tc := range []struct{ id, profile string }{
		{"cp-iso-eng-1", "engineering"},
		{"cp-iso-eng-2", "engineering"},
		{"cp-iso-sec-1", "security"},
	} {
		require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      "note",
			ProfileID: tc.profile,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Whichever profile a search names, it sees all three notes.
	for _, profile := range []string{"engineering", "security"} {
		resp := doGet(t, env.URL+"/api/v1/search?q=type==note&profile="+profile)
		var body searchResponse
		decodeJSON(t, resp.Body, &body)
		resp.Body.Close()
		assert.Equal(t, 3, body.Total, "profile %s", profile)
	}
}

// TestUS0053_AggregatedSearchViaHTTP verifies the REST endpoint returns objects
// across profiles when no profile filter is applied.
func TestUS0053_AggregatedSearchViaHTTP(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := ownerCtx()
	now := nowTrunc()

	for _, tc := range []struct{ id, profile string }{
		{"cp-http-a", "p1"},
		{"cp-http-b", "p2"},
	} {
		require.NoError(t, env.driver.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      "article",
			ProfileID: tc.profile,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	resp := doGet(t, env.URL+"/api/v1/search?q=type==article")
	defer resp.Body.Close()

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.Equal(t, 2, body.Total, "aggregated search must return both profiles' articles")
	assert.True(t, containsID(body.Data, "cp-http-a"), "profile p1 article")
	assert.True(t, containsID(body.Data, "cp-http-b"), "profile p2 article")
}
