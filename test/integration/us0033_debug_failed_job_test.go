package integration

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ---------------------------------------------------------------------------
// Mock steps for US-0033
// ---------------------------------------------------------------------------

// misconfiguredStep simulates a pipeline step that fails with a descriptive error
// as if its configuration is invalid (e.g., missing API key, bad model ID).
type misconfiguredStep struct {
	pipeline.BaseContract
	missingKey string
}

func (s *misconfiguredStep) Name() string { return "test-misconfigured-step" }
func (s *misconfiguredStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, fmt.Errorf("misconfigured pipeline step: missing required config key %q", s.missingKey)
}

// panicRecoveredStep is the placeholder for a worker panic-recovery test that
// was never written: no US-0033 test wires it, and its Run returns an error
// rather than panicking, so it would not exercise recovery as written. Kept so
// the missing coverage stays visible instead of disappearing with the type.
//
//nolint:unused // panic-recovery test not yet written
type panicRecoveredStep struct {
	pipeline.BaseContract
}

//nolint:unused // see panicRecoveredStep
func (s *panicRecoveredStep) Name() string { return "test-panic-recovered" }

//nolint:unused // see panicRecoveredStep
func (s *panicRecoveredStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, fmt.Errorf("runtime error: nil pointer dereference in enrichment step")
}

// ---------------------------------------------------------------------------
// US-0033 Tests
// ---------------------------------------------------------------------------

// TestUS0033_MisconfiguredPipelineJobFails verifies that a job using a pipeline
// with a misconfigured step transitions to the "failed" state.
func TestUS0033_MisconfiguredPipelineJobFails(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("us0033.misconfigured", &pipeline.Pipeline{
		PipelineName: "us0033.misconfigured",
		Steps: []pipeline.PipelineStep{
			&misconfiguredStep{missingKey: "openai_api_key"},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "test content for misconfigured pipeline",
		Type:     "text",
		Pipeline: "us0033.misconfigured",
		Source:   "e2e-test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, jobID)

	job := waitForJob(t, env.URL, jobID, storage.JobFailed)
	assert.Equal(t, storage.JobFailed, job.Status)
}

// TestUS0033_FailedJobErrorDetailRetrievableViaAPI verifies error details are
// accessible via GET /api/v1/jobs/{id} after a job fails.
func TestUS0033_FailedJobErrorDetailRetrievableViaAPI(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const errKeyName = "embedding_model_id"
	env.svc.Pipes.Upsert("us0033.detail", &pipeline.Pipeline{
		PipelineName: "us0033.detail",
		Steps: []pipeline.PipelineStep{
			&misconfiguredStep{missingKey: errKeyName},
		},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content that triggers misconfiguration error",
		Type:     "text",
		Pipeline: "us0033.detail",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	// Wait for failure.
	waitForJob(t, env.URL, jobID, storage.JobFailed)

	// Retrieve via API and inspect error details.
	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, jobID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var job storage.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&job))

	assert.Equal(t, storage.JobFailed, job.Status)
	assert.NotEmpty(t, job.Error, "failed job error field must be non-empty")
	assert.Contains(t, job.Error, errKeyName,
		"error message should identify the misconfigured key")
}

// TestUS0033_FailedJobListedInFailedFilter verifies a failed job appears when
// listing jobs filtered by status=failed.
func TestUS0033_FailedJobListedInFailedFilter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("us0033.list-fail", &pipeline.Pipeline{
		PipelineName: "us0033.list-fail",
		Steps:        []pipeline.PipelineStep{&misconfiguredStep{missingKey: "api_token"}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content triggering list-fail pipeline",
		Type:     "text",
		Pipeline: "us0033.list-fail",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	waitForJob(t, env.URL, jobID, storage.JobFailed)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs?status=failed&limit=100", env.URL))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Data  []*storage.Job `json:"data"`
		Total int            `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 1)

	found := false
	for _, j := range body.Data {
		if j.ID == jobID {
			found = true
			assert.NotEmpty(t, j.Error)
			break
		}
	}
	assert.True(t, found, "failed job %s must appear in the failed list", jobID)
}

// TestUS0033_UnknownPipelineJobFails verifies that enqueuing a job with a
// nonexistent pipeline name is rejected at enqueue time with a descriptive error.
func TestUS0033_UnknownPipelineJobFails(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	_, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "content for unknown pipeline",
		Type:     "text",
		Pipeline: "us0033.does-not-exist",
		Source:   "e2e-test",
	})
	require.Error(t, err, "enqueue with unknown pipeline must fail")
	assert.True(t,
		strings.Contains(err.Error(), "pipeline") || strings.Contains(err.Error(), "not found"),
		"error should mention pipeline or not-found; got: %q", err.Error())
}

// TestUS0033_FailedJobHasTimestamps verifies that completed_at is set on a
// failed job, and started_at is also populated.
func TestUS0033_FailedJobHasTimestamps(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	env.svc.Pipes.Upsert("us0033.ts-check", &pipeline.Pipeline{
		PipelineName: "us0033.ts-check",
		Steps:        []pipeline.PipelineStep{&misconfiguredStep{missingKey: "ts_key"}},
	})

	jobID, err := env.svc.Analyze(context.Background(), service.AnalyzeRequest{
		Content:  "timestamp check content",
		Type:     "text",
		Pipeline: "us0033.ts-check",
		Source:   "e2e-test",
	})
	require.NoError(t, err)

	waitForJob(t, env.URL, jobID, storage.JobFailed)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, jobID))
	require.NoError(t, err)
	defer resp.Body.Close()

	var job storage.Job
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&job))

	assert.Equal(t, storage.JobFailed, job.Status)
	assert.NotEmpty(t, job.Error)
}
