package integration

// US-0037: Agent Discovers Query Schema
//
// Tests schema discovery via GET /api/v1/query-schema.
// Note: The /query-schema endpoint is NOT yet implemented in the HTTP router.
// Tests validate what is available today (search endpoint structure) and stub
// the expected contract so the missing endpoint is clearly surfaced on failure.
//
// Gate: INTEGRATION=1 env var required to run.

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestUS0037_QuerySchemaEndpointReturns200 verifies GET /api/v1/query-schema returns 200.
//
// MISSING ENDPOINT: This test will fail until /api/v1/query-schema is registered in server.go.
func TestUS0037_QuerySchemaEndpointReturns200(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode,
		"GET /api/v1/query-schema must return 200 OK — endpoint not yet implemented")
}

// TestUS0037_QuerySchemaContentTypeIsJSON verifies response Content-Type is application/json.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaContentTypeIsJSON(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json")
}

// querySchema is the expected shape of the /query-schema response.
type querySchema struct {
	Version      string             `json:"version"`
	LastUpdated  string             `json:"lastUpdated"`
	Properties   []schemaProperty   `json:"properties"`
	Operators    []schemaOperator   `json:"operators"`
	Examples     []schemaExample    `json:"examples"`
	Constraints  *schemaConstraints `json:"constraints"`
	Deprecations []any              `json:"deprecations"`
}

type schemaProperty struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"`
	Indexed     bool            `json:"indexed"`
	Description string          `json:"description"`
	Values      []string        `json:"values,omitempty"`
	Examples    []schemaExample `json:"examples,omitempty"`
}

type schemaOperator struct {
	Symbol         string   `json:"symbol"`
	Name           string   `json:"name"`
	Usage          string   `json:"usage"`
	Example        string   `json:"example,omitempty"`
	SupportedTypes []string `json:"supported_types,omitempty"`
}

type schemaExample struct {
	Intent      string `json:"intent,omitempty"`
	RSQL        string `json:"rsql,omitempty"`
	Explanation string `json:"explanation,omitempty"`
	Usage       string `json:"usage,omitempty"`
	Description string `json:"description,omitempty"`
}

type schemaConstraints struct {
	MaxQueryLength int `json:"max_query_length"`
	MaxResults     int `json:"max_results"`
	TimeoutMS      int `json:"timeout_ms"`
}

// TestUS0037_QuerySchemaHasVersionField verifies schema includes version field.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaHasVersionField(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var schema querySchema
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&schema))

	assert.NotEmpty(t, schema.Version, "schema must include a version field")
}

// TestUS0037_QuerySchemaHasRequiredProperties verifies all documented fields are present.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaHasRequiredProperties(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var schema querySchema
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&schema))

	wantProps := []string{"type", "tags", "created_at", "mentions", "pipeline"}
	gotProps := make(map[string]bool)
	for _, p := range schema.Properties {
		gotProps[p.Name] = true
		assert.NotEmpty(t, p.Name, "property must have name")
		assert.NotEmpty(t, p.Type, "property %q must have type", p.Name)
		assert.NotEmpty(t, p.Description, "property %q must have description", p.Name)
	}

	for _, want := range wantProps {
		assert.True(t, gotProps[want], "schema must include property %q", want)
	}
}

// TestUS0037_QuerySchemaHasAllOperators verifies all 10 documented operators are present.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaHasAllOperators(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var schema querySchema
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&schema))

	wantOps := []string{"==", "!=", "<", ">", "<=", ">=", "=in=", "=out=", ";", ","}
	gotOps := make(map[string]bool)
	for _, op := range schema.Operators {
		gotOps[op.Symbol] = true
		assert.NotEmpty(t, op.Name, "operator %q must have name", op.Symbol)
		assert.NotEmpty(t, op.Usage, "operator %q must have usage", op.Symbol)
	}
	for _, want := range wantOps {
		assert.True(t, gotOps[want], "schema must include operator %q", want)
	}
}

// TestUS0037_QuerySchemaHasConstraints verifies constraints block is present.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaHasConstraints(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var schema querySchema
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&schema))

	require.NotNil(t, schema.Constraints, "schema must include constraints object")
	assert.Greater(t, schema.Constraints.MaxQueryLength, 0, "max_query_length must be positive")
	assert.Greater(t, schema.Constraints.MaxResults, 0, "max_results must be positive")
	assert.Greater(t, schema.Constraints.TimeoutMS, 0, "timeout_ms must be positive")
}

// TestUS0037_QuerySchemaRespondsWithinTimeout verifies response arrives within 5s.
//
// MISSING ENDPOINT: Will fail until /api/v1/query-schema is implemented.
func TestUS0037_QuerySchemaRespondsWithinTimeout(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	start := time.Now()
	req, err := gohttp.NewRequest(gohttp.MethodGet, env.URL+"/api/v1/query-schema", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "application/json")

	resp, err := gohttp.DefaultClient.Do(req)
	elapsed := time.Since(start)
	require.NoError(t, err)
	resp.Body.Close()

	require.Equal(t, gohttp.StatusOK, resp.StatusCode)
	assert.Less(t, elapsed, 5*time.Second, "schema response must arrive within 5s")
}

// TestUS0037_SearchEndpointAcceptsRSQLQuery verifies the existing search endpoint
// accepts an RSQL query, confirming agent can enumerate results even without
// a formal /query-schema endpoint.
//
// This test validates what IS working today as a fallback until /query-schema lands.
func TestUS0037_SearchEndpointAcceptsRSQLQuery(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := nowTrunc()

	// Seed objects of different types so the agent can discover what's queryable.
	for _, tc := range []struct{ id, typ string }{
		{"schema-obj-1", "article"},
		{"schema-obj-2", "decision"},
		{"schema-obj-3", "note"},
	} {
		require.NoError(t, env.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
			ID:        tc.id,
			Type:      tc.typ,
			CreatedAt: now,
			UpdatedAt: now,
		}))
	}

	// Agent uses type== operator to filter — validates RSQL operator is understood.
	resp := doGet(t, fmt.Sprintf("%s/api/v1/search?q=type==article", env.URL))
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode,
		"search endpoint must return 200 for valid RSQL type filter")

	var body searchResponse
	decodeJSON(t, resp.Body, &body)
	assert.Equal(t, 1, body.Total)
	assert.Equal(t, "schema-obj-1", body.Data[0].ID)
}
