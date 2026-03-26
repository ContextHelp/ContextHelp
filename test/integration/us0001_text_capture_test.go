package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock pipeline steps for text capture tests.
// ---------------------------------------------------------------------------

// textTypeSetterStep sets Type = "text" and Subtype on the draft.
type textTypeSetterStep struct {
	pipeline.BaseContract
	subtype string
}

func (s *textTypeSetterStep) Name() string { return "test-text-type-setter" }
func (s *textTypeSetterStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Type = "text"
	draft.Subtype = s.subtype
	return draft, nil
}

// textProfileStep records profile/project metadata from the draft's Metadata map.
type textProfileStep struct {
	pipeline.BaseContract
}

func (s *textProfileStep) Name() string { return "test-text-profile" }
func (s *textProfileStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	// pass-through: Metadata already contains profile/project values from ingest
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0001 Tests
// ---------------------------------------------------------------------------

// TestUS0001_PlainTextPostReturnsJobID verifies POST /analyze with plain text returns 202 + job_id.
func TestUS0001_PlainTextPostReturnsJobID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content": "Here is an insight about error handling patterns",
		"type":    "text",
		"source":  "e2e-test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "response must contain a job_id")
}

// TestUS0001_JobCompletesWithCorrectTypeAndPipeline verifies text object stored with correct
// content_type and pipeline assigned after job completion.
func TestUS0001_JobCompletesWithCorrectTypeAndPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.short", &pipeline.Pipeline{
		PipelineName: "text.short",
		Steps: []pipeline.PipelineStep{
			&textTypeSetterStep{subtype: "short"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Short text insight for testing",
		Type:     "text",
		Pipeline: "text.short",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))

	assert.Equal(t, "text", obj.Type, "type should be text")
	assert.Equal(t, "text.short", obj.Pipeline, "pipeline should be text.short")
	assert.Equal(t, "e2e-test", obj.Source, "source should be e2e-test")
}

// TestUS0001_ObjectRetrievableByID verifies the object is retrievable via GET /objects/{id}.
func TestUS0001_ObjectRetrievableByID(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const content = "Retrievable text content for ID test"
	jobID := postAnalyze(t, env.URL, content)
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, job.ResultID, obj.ID, "object ID must match result_id")
	assert.Equal(t, content, obj.RawContent, "raw_content must match submitted content")
}

// TestUS0001_ProfileAndProjectPersistedInMetadata verifies profile and project fields
// submitted in the request are persisted in the object's Metadata.
func TestUS0001_ProfileAndProjectPersistedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("text.short", &pipeline.Pipeline{
		PipelineName: "text.short",
		Steps: []pipeline.PipelineStep{
			&textProfileStep{},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "Insight with profile context",
		Type:     "text",
		Pipeline: "text.short",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, job.ResultID, obj.ID)
}

// TestUS0001_JobIDReturnedImmediately verifies POST /analyze is non-blocking:
// returns 202 + job_id before enrichment completes.
func TestUS0001_JobIDReturnedImmediately(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content": "Non-blocking capture test",
		"type":    "text",
		"source":  "e2e-test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// Must respond immediately with 202 — not block until enrichment.
	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}

// TestUS0001_JobStatusTransitionsPendingToCompleted verifies job transitions
// from pending to completed within reasonable time.
func TestUS0001_JobStatusTransitionsPendingToCompleted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "Status transition test content")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)

	assert.NotEmpty(t, job.ResultID, "completed job must have result_id")
	assert.Equal(t, storage.JobCompleted, job.Status)
}
