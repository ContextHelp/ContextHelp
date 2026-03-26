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
// Mock steps used in custom pipeline registration tests.
// ---------------------------------------------------------------------------

// customTagStep tags the object with a marker to prove the custom step ran.
type customTagStep struct {
	pipeline.BaseContract
	tag string
}

func (s *customTagStep) Name() string { return "test-custom-tag" }
func (s *customTagStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	draft.Tags = append(draft.Tags, storage.Tag{Label: s.tag})
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["custom_step_ran"] = true
	return draft, nil
}

// customSummaryStep simulates a custom summarization step.
type customSummaryStep struct {
	pipeline.BaseContract
	summary string
}

func (s *customSummaryStep) Name() string { return "test-custom-summary" }
func (s *customSummaryStep) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["summary"] = s.summary
	return draft, nil
}

// ---------------------------------------------------------------------------
// US-0028 Tests
// ---------------------------------------------------------------------------

// TestUS0028_RegisterPipelineViaAPI verifies that POST /api/v1/pipelines creates a
// custom pipeline and returns its id.
func TestUS0028_RegisterPipelineViaAPI(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "my-custom-pipeline",
		"description": "Custom pipeline for US-0028 test",
		"steps":       stepsJSON,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["id"], "create must return a non-empty pipeline id")
}

// TestUS0028_RegisteredPipelineRetrievableByName verifies that after creation
// GET /api/v1/pipelines/{name} returns the pipeline with correct fields.
func TestUS0028_RegisteredPipelineRetrievableByName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	createBody, _ := json.Marshal(map[string]string{
		"name":        "retrievable-pipeline",
		"description": "Pipeline to verify retrieval",
		"steps":       stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	var createResult map[string]string
	require.NoError(t, json.NewDecoder(respCreate.Body).Decode(&createResult))
	createdID := createResult["id"]

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/retrievable-pipeline")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pl storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pl))

	assert.Equal(t, "retrievable-pipeline", pl.Name)
	assert.Equal(t, "Pipeline to verify retrieval", pl.Description)
	assert.Equal(t, createdID, pl.ID, "id in GET must match id returned on create")
	assert.False(t, pl.IsBuiltIn, "custom pipeline must have is_built_in=false")
	assert.False(t, pl.Archived, "new pipeline must not be archived")
}

// TestUS0028_RegisteredPipelineAppearsInListing verifies that after creation the
// pipeline appears in GET /api/v1/pipelines.
func TestUS0028_RegisteredPipelineAppearsInListing(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	createBody, _ := json.Marshal(map[string]string{
		"name":  "listing-pipeline",
		"steps": stepsJSON,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var listBody struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&listBody))

	found := false
	for _, pl := range listBody.Pipelines {
		if pl.Name == "listing-pipeline" {
			found = true
			break
		}
	}
	assert.True(t, found, "newly created pipeline must appear in the listing")
}

// TestUS0028_IngestWithCustomPipelineNameExecutesSteps verifies that enqueueing
// content with a custom pipeline name routes through the registered in-memory steps.
func TestUS0028_IngestWithCustomPipelineNameExecutesSteps(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("custom.exec", &pipeline.Pipeline{
		PipelineName: "custom.exec",
		Steps: []pipeline.PipelineStep{
			&customTagStep{tag: "custom-step-executed"},
			&customSummaryStep{summary: "custom summary output"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content for custom pipeline",
		Type:     "text",
		Pipeline: "custom.exec",
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

	// Verify the custom tag step ran by checking tag labels.
	found := false
	for _, tag := range obj.Tags {
		if tag.Label == "custom-step-executed" {
			found = true
			break
		}
	}
	assert.True(t, found, "custom tag step must have appended the marker tag")
	assert.Equal(t, true, obj.Metadata["custom_step_ran"], "custom step ran flag must be set")
	assert.Equal(t, "custom summary output", obj.Metadata["summary"], "custom summary step result must be stored")
}

// TestUS0028_MissingNameReturns400 verifies server rejects pipeline creation without a name.
func TestUS0028_MissingNameReturns400(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"steps": `[{"type":"text_summary"}]`,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

// TestUS0028_MissingStepsReturns400 verifies server rejects pipeline creation without steps.
func TestUS0028_MissingStepsReturns400(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"name": "no-steps-pipeline",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

// TestUS0028_EnqueueViaHTTPRecordsJobPipeline verifies that the enqueue endpoint
// stores the pipeline name on the resulting job record.
func TestUS0028_EnqueueViaHTTPRecordsJobPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("enqueue-test-pl", &pipeline.Pipeline{
		PipelineName: "enqueue-test-pl",
		Steps:        []pipeline.PipelineStep{&customTagStep{tag: "enqueued"}},
	})

	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "test enqueue content",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "enqueue-test-pl",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result["job_id"])

	job := waitForJob(t, env.URL, result["job_id"], storage.JobCompleted)
	assert.Equal(t, "enqueue-test-pl", job.Pipeline, "job must record the pipeline name")
}
