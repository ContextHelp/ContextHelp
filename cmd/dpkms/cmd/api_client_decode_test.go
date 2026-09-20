package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// jsonServer serves body with a 200 and application/json.
func jsonServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestAPIClient_ListPipelines_MalformedEntry_Errors confirms a pipelines
// entry the client cannot decode is reported instead of being dropped.
//
// Before the fix the decode error was discarded and the call returned
// (nil, total, nil): an empty list presented as a successful response,
// so "no pipelines" and "the payload was unreadable" looked identical.
func TestAPIClient_ListPipelines_MalformedEntry_Errors(t *testing.T) {
	// "pipelines" decodes into []*storage.Pipeline, so a list of strings
	// re-marshals cleanly and then fails on the typed decode.
	ts := jsonServer(t, `{"pipelines":["not-a-pipeline-object"],"total":1}`)

	c := NewAPIClient(ts.URL)
	pipelines, total, err := c.ListPipelines(storage.PipelineFilter{})
	require.Error(t, err, "malformed pipelines payload must surface, got %d pipelines", len(pipelines))
	assert.Contains(t, err.Error(), "decode pipelines")
	assert.Nil(t, pipelines)
	assert.Zero(t, total)
}

// TestAPIClient_ListSteps_MalformedEntry_Errors is the same contract for
// the steps listing.
func TestAPIClient_ListSteps_MalformedEntry_Errors(t *testing.T) {
	ts := jsonServer(t, `{"steps":["not-a-step-object"],"total":1}`)

	c := NewAPIClient(ts.URL)
	steps, total, err := c.ListSteps("")
	require.Error(t, err, "malformed steps payload must surface, got %d steps", len(steps))
	assert.Contains(t, err.Error(), "decode steps")
	assert.Nil(t, steps)
	assert.Zero(t, total)
}

// TestAPIClient_ListRegistries_MalformedEntry_Errors is the same
// contract for the registries listing.
func TestAPIClient_ListRegistries_MalformedEntry_Errors(t *testing.T) {
	ts := jsonServer(t, `{"registries":["not-a-registry-object"],"total":1}`)

	c := NewAPIClient(ts.URL)
	registries, total, err := c.ListRegistries()
	require.Error(t, err, "malformed registries payload must surface, got %d registries", len(registries))
	assert.Contains(t, err.Error(), "decode registries")
	assert.Nil(t, registries)
	assert.Zero(t, total)
}

// TestAPIClient_ListPipelines_WellFormed_Succeeds guards the fix against
// over-reach: a valid payload must still decode.
func TestAPIClient_ListPipelines_WellFormed_Succeeds(t *testing.T) {
	ts := jsonServer(t, `{"pipelines":[{"name":"p-1"}],"total":1}`)

	c := NewAPIClient(ts.URL)
	pipelines, total, err := c.ListPipelines(storage.PipelineFilter{})
	require.NoError(t, err)
	require.Len(t, pipelines, 1)
	assert.Equal(t, "p-1", pipelines[0].Name)
	assert.Equal(t, 1, total)
}

// TestAPIClient_ParseError_NonJSONBody_ReportsRawBody confirms the one
// deliberately tolerated decode: parseError is already on the error
// path, so a body that is not the daemon's JSON error envelope is
// reported verbatim rather than being swallowed or replaced.
func TestAPIClient_ParseError_NonJSONBody_ReportsRawBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>gateway exploded</html>"))
	}))
	t.Cleanup(ts.Close)

	c := NewAPIClient(ts.URL)
	_, _, err := c.ListPipelines(storage.PipelineFilter{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gateway exploded")
}
