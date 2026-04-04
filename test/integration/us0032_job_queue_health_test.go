package integration

import (
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
// Mock steps for US-0032
// ---------------------------------------------------------------------------

// alwaysFailStep is a pipeline step that always returns an error.
type alwaysFailStep struct {
	pipeline.BaseContract
}

func (s *alwaysFailStep) Name() string { return "test-always-fail" }
func (s *alwaysFailStep) Run(_ context.Context, _ *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	return nil, pipeline.Permanent(fmt.Errorf("intentional failure for test"))
}

// ---------------------------------------------------------------------------
// US-0032 Tests
// ---------------------------------------------------------------------------

// TestUS0032_HealthReturnsOK verifies /health returns 200 with status=ok.
func TestUS0032_HealthReturnsOK(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

// TestUS0032_JobCounts enqueues a mix of successful and intentionally failing
// jobs in one shared environment, then verifies the job listing counts are
// correct by status.
func TestUS0032_JobCounts(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()

	// Register a pipeline that always fails.
	env.svc.Pipes.Upsert("us0032.fail", &pipeline.Pipeline{
		PipelineName: "us0032.fail",
		Steps:        []pipeline.PipelineStep{&alwaysFailStep{}},
	})

	// --- enqueue successful jobs ---
	const nOK = 5
	okIDs := make([]string, nOK)
	for i := 0; i < nOK; i++ {
		okIDs[i] = postAnalyze(t, env.URL, fmt.Sprintf("ok-queue-content-%d", i))
	}

	// --- enqueue intentionally failing jobs ---
	const nFail = 3
	failIDs := make([]string, nFail)
	for i := 0; i < nFail; i++ {
		id, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
			Content:  fmt.Sprintf("fail-queue-content-%d", i),
			Type:     "text",
			Pipeline: "us0032.fail",
			Source:   "e2e-test",
		})
		require.NoError(t, err)
		failIDs[i] = id
	}

	// Wait for all to reach terminal state.
	for _, id := range okIDs {
		waitForJob(t, env.URL, id, storage.JobCompleted)
	}
	for _, id := range failIDs {
		job := waitForJob(t, env.URL, id, storage.JobFailed)
		assert.NotEmpty(t, job.Error, "failed job must carry an error message")
	}

	// Verify completed count via API.
	completedResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs?status=completed&limit=100", env.URL))
	require.NoError(t, err)
	defer completedResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, completedResp.StatusCode)

	var completedBody struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(completedResp.Body).Decode(&completedBody))
	assert.GreaterOrEqual(t, completedBody.Total, nOK, "completed count must include all successful jobs")

	// Verify failed count via API.
	failedResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs?status=failed&limit=100", env.URL))
	require.NoError(t, err)
	defer failedResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, failedResp.StatusCode)

	var failedBody struct {
		Data  []*storage.Job `json:"data"`
		Total int            `json:"total"`
	}
	require.NoError(t, json.NewDecoder(failedResp.Body).Decode(&failedBody))
	assert.GreaterOrEqual(t, failedBody.Total, nFail, "failed count must include all failing jobs")
	for _, job := range failedBody.Data {
		assert.NotEmpty(t, job.Error, "each failed job must have a non-empty error field")
	}

	// Verify overall listing includes both sets.
	allResp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/jobs?limit=100", env.URL))
	require.NoError(t, err)
	defer allResp.Body.Close()

	var allBody struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(allResp.Body).Decode(&allBody))
	assert.GreaterOrEqual(t, allBody.Total, nOK+nFail,
		"total job count must include all enqueued jobs")
}
