package integration

// US-0040: Agent Composes Brief Programmatically
//
// Tests the compose API from an agent's perspective.
// The HTTP /compose endpoint is NOT yet implemented. Tests use svc.Compose and
// svc.ComposeWithCitations directly (same service layer the future HTTP handler
// will call) and document the expected HTTP contract with stub tests.
//
// Gate: INTEGRATION=1 env var required to run.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers / mock steps for compose tests.
// ---------------------------------------------------------------------------

// agentComposeObjectStep seeds a knowledge object with known summary content
// simulating a fully-enriched object ready for composition.
type agentComposeObjectStep struct {
	pipeline.BaseContract
	summary string
	objType string
}

func (s *agentComposeObjectStep) Name() string { return "test-agent-compose-object" }
func (s *agentComposeObjectStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.objType != "" {
		draft.Type = s.objType
	}
	draft.Summaries = []string{s.summary}
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	return draft, nil
}

// ingestObjectForCompose ingests content through a mock pipeline that sets a known
// summary, then waits for job completion and returns the stored object.
func ingestObjectForCompose(t *testing.T, env *testEnv, content, summary, objType string) *storage.KnowledgeObject {
	t.Helper()
	pipeName := fmt.Sprintf("agent.compose.%d", uniqueID())
	env.svc.Pipes.Upsert(pipeName, &pipeline.Pipeline{
		PipelineName: pipeName,
		Steps: []pipeline.PipelineStep{
			&agentComposeObjectStep{summary: summary, objType: objType},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  content,
		Type:     objType,
		Pipeline: pipeName,
		Source:   "agent-compose-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	obj, err := env.svc.Store.Objects().Get(context.Background(), job.ResultID)
	require.NoError(t, err)
	return obj
}

var uniqueCounter int

func uniqueID() int {
	uniqueCounter++
	return uniqueCounter
}

// ---------------------------------------------------------------------------
// US-0040 Tests: service-layer (works today)
// ---------------------------------------------------------------------------

// TestUS0040_ComposeReturnsBriefWithObjectContent verifies Compose returns
// markdown containing the ingested objects' summary content.
func TestUS0040_ComposeReturnsBriefWithObjectContent(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj1 := ingestObjectForCompose(t, env,
		"Backend architecture decision notes",
		"We decided to adopt event-driven microservices.",
		"decision",
	)
	obj2 := ingestObjectForCompose(t, env,
		"Frontend migration briefing",
		"Frontend will migrate from React class components to hooks.",
		"article",
	)

	brief, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{obj1, obj2}, "brief")
	require.NoError(t, err)
	require.NotEmpty(t, brief)

	assert.Contains(t, brief, "event-driven microservices",
		"brief must contain summary from first object")
	assert.Contains(t, brief, "hooks",
		"brief must contain summary from second object")
}

// TestUS0040_ComposeBriefIsMarkdown verifies the brief output contains markdown structure.
func TestUS0040_ComposeBriefIsMarkdown(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj := ingestObjectForCompose(t, env,
		"Security audit findings",
		"TLS 1.3 adoption confirmed across all services.",
		"article",
	)

	brief, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{obj}, "brief")
	require.NoError(t, err)

	// Output must be markdown — starts with a # header.
	assert.True(t, strings.HasPrefix(brief, "# "),
		"brief must start with a markdown # header; got: %q", brief[:min(50, len(brief))])
}

// TestUS0040_ComposeWithCitationsIncludesSourceIDs verifies ComposeWithCitations
// returns a sources array listing all contributing object IDs.
func TestUS0040_ComposeWithCitationsIncludesSourceIDs(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj1 := ingestObjectForCompose(t, env, "Auth service decision", "SSO via OIDC selected.", "decision")
	obj2 := ingestObjectForCompose(t, env, "Auth service plan", "Rollout in two phases.", "article")

	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{obj1, obj2}, "brief")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Contains(t, result.SourceIDs, obj1.ID,
		"sources must include first object ID (provenance)")
	assert.Contains(t, result.SourceIDs, obj2.ID,
		"sources must include second object ID (provenance)")
}

// TestUS0040_ComposeWithCitationsStructuredNotMarkdownBlob verifies composition
// result is a structured object (has Content, Citations, SourceIDs, Type) —
// not a raw markdown string blob.
func TestUS0040_ComposeWithCitationsStructuredNotMarkdownBlob(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj := ingestObjectForCompose(t, env, "Platform decision", "Docker Compose for local dev.", "decision")

	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{obj}, "brief")
	require.NoError(t, err)
	require.NotNil(t, result, "compose result must not be nil")

	assert.NotEmpty(t, result.Content, "result.Content must not be empty")
	assert.NotEmpty(t, result.Type, "result.Type must not be empty")
	assert.NotNil(t, result.SourceIDs, "result.SourceIDs must not be nil")
	assert.NotNil(t, result.Citations, "result.Citations must not be nil")
}

// TestUS0040_ComposeQueryMatchTemplate verifies template value is preserved in the result type.
func TestUS0040_ComposeQueryMatchTemplate(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj := ingestObjectForCompose(t, env, "Release plan", "Ship v2 by end of quarter.", "article")

	result, err := env.svc.ComposeWithCitations(ctx, []*storage.KnowledgeObject{obj}, "plan")
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, "plan", result.Type,
		"composition type must match the requested template")
}

// TestUS0040_ComposeEmptyObjectsReturnsEmptyBrief verifies composing from zero objects
// produces an informative response, not a panic or silent empty string.
func TestUS0040_ComposeEmptyObjectsReturnsEmptyBrief(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	brief, err := env.svc.Compose(context.Background(), []*storage.KnowledgeObject{}, "brief")
	require.NoError(t, err)

	// Must be informative — not a silent empty string.
	assert.Contains(t, brief, "0 knowledge objects",
		"brief from empty object list must state zero objects")
}

// TestUS0040_ComposeObjectsQueryableAfterCompose verifies that composed-from objects
// remain queryable via search after composition (no mutation side-effect).
func TestUS0040_ComposeObjectsQueryableAfterCompose(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	obj := ingestObjectForCompose(t, env,
		"Compose side-effect test content",
		"No mutation expected after composition.",
		"article",
	)

	// Compose.
	_, err := env.svc.Compose(ctx, []*storage.KnowledgeObject{obj}, "brief")
	require.NoError(t, err)

	// Object must still be searchable.
	objs, total, err := env.svc.SearchObjects(ctx, "type==article", 20, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, total, 1)
	assert.True(t, containsIDPtr(objs, obj.ID),
		"composed-from object must remain in search results after composition")
}

// ---------------------------------------------------------------------------
// US-0040 Tests: HTTP endpoint stub (MISSING — documents expected contract)
// ---------------------------------------------------------------------------

// TestUS0040_HTTPComposeEndpointExists verifies POST /api/v1/compose is registered.
//
// MISSING ENDPOINT: Will fail until /api/v1/compose is registered in server.go.
func TestUS0040_HTTPComposeEndpointExists(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"query":         "type==decision",
		"template":      "brief",
		"output_format": "markdown",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/compose", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Must return 202 Accepted with job_id + composition_id.
	// Currently returns 404 (endpoint not registered) — test documents the gap.
	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode,
		"POST /api/v1/compose must return 202 Accepted — endpoint not yet implemented")
}

// TestUS0040_HTTPComposeReturnsJobIDAndCompositionID verifies 202 body has job_id + composition_id.
//
// MISSING ENDPOINT: Will fail until /api/v1/compose is implemented.
func TestUS0040_HTTPComposeReturnsJobIDAndCompositionID(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"query":         "type==decision",
		"template":      "brief",
		"output_format": "markdown",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/compose", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.NotEmpty(t, result["job_id"], "202 body must contain job_id")
	assert.NotEmpty(t, result["composition_id"], "202 body must contain composition_id")
}

// TestUS0040_HTTPComposeMissingTemplatereturns400 verifies missing template = 400.
//
// MISSING ENDPOINT: Will fail until /api/v1/compose is implemented.
func TestUS0040_HTTPComposeMissingTemplateReturns400(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	// No template field.
	body, _ := json.Marshal(map[string]string{
		"query":         "type==decision",
		"output_format": "markdown",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/compose", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode,
		"POST /api/v1/compose with missing template must return 400")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
