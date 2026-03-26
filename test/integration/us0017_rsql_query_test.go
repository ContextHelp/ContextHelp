package integration

// US-0017: Structured RSQL Query
// Ingests objects with known metadata, runs RSQL filter expressions, verifies
// exact result sets.

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUS0017_RSQLTypeFilter verifies that type==<value> returns only matching objects.
func TestUS0017_RSQLTypeFilter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"rsql-dec-1", "decision"},
		{"rsql-dec-2", "decision"},
		{"rsql-note-1", "note"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	objs, total, err := env.svc.SearchObjects(ctx, "type==decision", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, objs, 2)
	for _, o := range objs {
		assert.Equal(t, "decision", o.Type)
	}
}

// TestUS0017_RSQLMultipleTypes verifies OR operator returns union of types.
func TestUS0017_RSQLMultipleTypes(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"rsql-multi-a", "article"},
		{"rsql-multi-b", "note"},
		{"rsql-multi-c", "decision"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	objs, total, err := env.svc.SearchObjects(ctx, "type=in=(article,note)", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total)
	assert.Len(t, objs, 2)
	for _, o := range objs {
		assert.Contains(t, []string{"article", "note"}, o.Type)
	}
}

// TestUS0017_RSQLInvalidExpressionReturnsError verifies that a malformed RSQL
// expression returns a parse error, not a partial result set.
func TestUS0017_RSQLInvalidExpressionReturnsError(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	_, _, err := env.svc.SearchObjects(ctx, "!!invalid!!", 20, 0)
	require.Error(t, err, "malformed RSQL must return an error")
}

// TestUS0017_RSQLViaHTTPEndpoint verifies the REST search endpoint routes RSQL
// correctly and returns matching objects.
func TestUS0017_RSQLViaHTTPEndpoint(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "rsql-http-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "rsql-http-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	}))

	resp := doGet(t, env.URL+"/api/v1/search?q=type==article")
	defer resp.Body.Close()

	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	require.Equal(t, 1, body.Total)
	assert.Equal(t, "rsql-http-1", body.Data[0].ID)
}

// TestUS0017_RSQLProfileScopeRestrictsResults verifies that profile_id scoping
// restricts the RSQL result set to the named profile.
func TestUS0017_RSQLProfileScopeRestrictsResults(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "rsql-prof-a", Type: "note", ProfileID: "alpha", CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "rsql-prof-b", Type: "note", ProfileID: "beta", CreatedAt: now, UpdatedAt: now,
	}))

	// Scoped to profile "alpha" — must return only rsql-prof-a.
	objs, total, err := env.svc.SearchObjects(ctx, "type==note", 20, 0, "alpha")
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, objs, 1)
	assert.Equal(t, "rsql-prof-a", objs[0].ID)
}
