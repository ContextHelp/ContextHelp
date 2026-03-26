package integration

// US-0038: Agent Constructs RSQL Query
//
// Tests agent-style RSQL query construction and execution via GET /api/v1/search.
// Verifies deterministic, exact-match semantics; AND/OR operators; empty results;
// and metadata presence on returned objects.
//
// Gate: INTEGRATION=1 env var required to run.

import (
	"context"
	"fmt"
	gohttp "net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestUS0038_AgentRSQLTypeEquality verifies type==value returns only matching objects.
func TestUS0038_AgentRSQLTypeEquality(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"rsql38-dec-1", "decision"},
		{"rsql38-dec-2", "decision"},
		{"rsql38-art-1", "article"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	resp := doGet(t, fmt.Sprintf("%s/api/v1/search?q=type==decision", env.URL))
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	assert.Equal(t, 2, body.Total, "type==decision must return exactly 2 results")
	assert.Len(t, body.Data, 2)
	for _, obj := range body.Data {
		assert.Equal(t, "decision", obj.Type)
	}
}

// TestUS0038_AgentRSQLAndOperator verifies semicolon AND combines type + source filters.
func TestUS0038_AgentRSQLAndOperator(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	objects := []storage.KnowledgeObject{
		{ID: "rsql38-and-1", Type: "article", Source: "agent", CreatedAt: now, UpdatedAt: now},
		{ID: "rsql38-and-2", Type: "article", Source: "manual", CreatedAt: now, UpdatedAt: now},
		{ID: "rsql38-and-3", Type: "note", Source: "agent", CreatedAt: now, UpdatedAt: now},
	}
	for i := range objects {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &objects[i]))
	}

	// AND: type==article;source==agent — only rsql38-and-1 matches.
	objs, total, err := env.svc.SearchObjects(ctx, "type==article;source==agent", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, objs, 1)
	assert.Equal(t, "rsql38-and-1", objs[0].ID)
}

// TestUS0038_AgentRSQLOrOperator verifies comma OR returns union across types.
func TestUS0038_AgentRSQLOrOperator(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"rsql38-or-1", "article"},
		{"rsql38-or-2", "decision"},
		{"rsql38-or-3", "note"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// OR: type==article,type==decision
	resp := doGet(t, fmt.Sprintf("%s/api/v1/search?q=%s",
		env.URL, "type%3D%3Darticle%2Ctype%3D%3Ddecision"))
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	assert.Equal(t, 2, body.Total, "type==article,type==decision must return 2")
	for _, obj := range body.Data {
		assert.Contains(t, []string{"article", "decision"}, obj.Type)
	}
}

// TestUS0038_AgentRSQLInOperator verifies =in= membership operator.
func TestUS0038_AgentRSQLInOperator(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, tc := range []struct{ id, typ string }{
		{"rsql38-in-1", "article"},
		{"rsql38-in-2", "note"},
		{"rsql38-in-3", "video"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// =in= operator: type=in=(article,note)
	objs, total, err := env.svc.SearchObjects(ctx, "type=in=(article,note)", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, total, "type=in=(article,note) must return 2 objects")
	for _, obj := range objs {
		assert.Contains(t, []string{"article", "note"}, obj.Type)
	}
}

// TestUS0038_AgentRSQLEmptyResultsReturn200 verifies no-match query returns 200 + empty array.
func TestUS0038_AgentRSQLEmptyResultsReturn200(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	resp := doGet(t, env.URL+"/api/v1/search?q=type==nonexistent_type_xyz")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "no-match RSQL must return 200, not 404")

	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	assert.Equal(t, 0, body.Total, "total must be 0 for no-match query")
	assert.Empty(t, body.Data, "data array must be empty for no-match query")
}

// TestUS0038_AgentRSQLInvalidExpressionReturns400 verifies malformed RSQL returns 400.
func TestUS0038_AgentRSQLInvalidExpressionReturns400(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	resp := doGet(t, env.URL+"/api/v1/search?q=!!invalid!!")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode,
		"malformed RSQL must return 400 Bad Request")
}

// TestUS0038_AgentRSQLDeterministicOrdering verifies same RSQL returns same order on repeat calls.
func TestUS0038_AgentRSQLDeterministicOrdering(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for _, id := range []string{"rsql38-det-c", "rsql38-det-a", "rsql38-det-b"} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        id,
			Type:      "article",
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	collect := func() []string {
		resp := doGet(t, env.URL+"/api/v1/search?q=type==article")
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusOK, resp.StatusCode)
		var body searchResponse
		decodeJSON(t, resp.Body, &body)
		ids := make([]string, len(body.Data))
		for i, obj := range body.Data {
			ids[i] = obj.ID
		}
		return ids
	}

	first := collect()
	second := collect()

	require.Len(t, first, 3)
	assert.Equal(t, first, second, "same RSQL must return same result order on repeat calls")
}

// TestUS0038_AgentRSQLObjectMetadataPresent verifies result objects carry id, type, source, created_at.
func TestUS0038_AgentRSQLObjectMetadataPresent(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:        "rsql38-meta-1",
		Type:      "article",
		Source:    "agent-test",
		Pipeline:  "text.short",
		CreatedAt: now,
		UpdatedAt: now,
	}))

	resp := doGet(t, env.URL+"/api/v1/search?q=type==article")
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	require.GreaterOrEqual(t, body.Total, 1)
	found := false
	for _, obj := range body.Data {
		if obj.ID == "rsql38-meta-1" {
			found = true
			assert.Equal(t, "article", obj.Type, "type must be present")
			assert.Equal(t, "agent-test", obj.Source, "source must be present")
			assert.Equal(t, "text.short", obj.Pipeline, "pipeline must be present")
			assert.False(t, obj.CreatedAt.IsZero(), "created_at must be non-zero")
			break
		}
	}
	assert.True(t, found, "seeded object must appear in search results")
}

// TestUS0038_AgentRSQLViaHTTPQueryMode verifies the search endpoint accepts
// a query_mode=rsql parameter (documents the expected future contract).
//
// NOTE: query_mode parameter is not yet parsed by the server — this test verifies
// the endpoint still returns 200 when the extra param is present (it is silently ignored).
func TestUS0038_AgentRSQLViaHTTPQueryMode(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "rsql38-qm-1", Type: "note", CreatedAt: now, UpdatedAt: now,
	}))

	// query_mode=rsql param — must not break the endpoint.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==note&query_mode=rsql")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode,
		"search endpoint must return 200 when query_mode=rsql is present")
}

// TestUS0038_AgentRSQLObjectRetrievableByID verifies object from search result
// is retrievable via GET /api/v1/objects/{id} (storage round-trip).
func TestUS0038_AgentRSQLObjectRetrievableByID(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID:        "rsql38-rt-1",
		Type:      "decision",
		Source:    "agent-test",
		CreatedAt: now,
		UpdatedAt: now,
	}))

	// Find via RSQL.
	resp := doGet(t, env.URL+"/api/v1/search?q=type==decision")
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	var body searchResponse
	decodeJSON(t, resp.Body, &body)

	require.GreaterOrEqual(t, body.Total, 1)
	require.True(t, containsID(body.Data, "rsql38-rt-1"), "search must return seeded object")

	// Retrieve by ID — confirms storage persistence.
	objResp := doGet(t, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, "rsql38-rt-1"))
	defer objResp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, objResp.StatusCode,
		"object from RSQL result must be retrievable via GET /objects/{id}")
}

// TestUS0038_AgentRSQLPerformanceLocal verifies local query round-trip < 500ms.
func TestUS0038_AgentRSQLPerformanceLocal(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	for i := 0; i < 10; i++ {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        fmt.Sprintf("rsql38-perf-%d", i),
			Type:      "article",
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	start := time.Now()
	resp := doGet(t, env.URL+"/api/v1/search?q=type==article")
	elapsed := time.Since(start)
	resp.Body.Close()

	assert.Less(t, elapsed, 500*time.Millisecond,
		"local RSQL query round-trip must be < 500ms; got %v", elapsed)
}
