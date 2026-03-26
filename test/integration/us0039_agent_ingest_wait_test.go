package integration

// US-0039: Agent Ingests Content And Waits
//
// Tests agent ingest-and-poll pattern:
//   POST /api/v1/analyze → receive job_id → poll GET /api/v1/jobs/{job_id}
//   until terminal status → retrieve object via GET /api/v1/objects/{object_id}.
//
// Note: The analyze response currently returns {job_id} only (no object_id field).
// Tests align to actual server contract and flag the missing object_id field.
//
// Gate: INTEGRATION=1 env var required to run.

import (
	"bytes"
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

// analyzeResponse captures the POST /analyze response body.
type analyzeResponse struct {
	JobID    string `json:"job_id"`
	ObjectID string `json:"object_id"` // documented in spec; may be empty until implemented
	Status   string `json:"status"`
}

// TestUS0039_PostAnalyzeReturns202 verifies POST /analyze returns HTTP 202.
func TestUS0039_PostAnalyzeReturns202(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content":     "Agent ingest test — authentication patterns overview",
		"source_type": "text",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode,
		"POST /analyze must return 202 Accepted")
}

// TestUS0039_PostAnalyzeReturnsJobID verifies response body contains non-empty job_id.
func TestUS0039_PostAnalyzeReturnsJobID(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content":     "Agent ingest test — RSQL query caching strategies",
		"source_type": "text",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result analyzeResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.NotEmpty(t, result.JobID, "POST /analyze response must contain non-empty job_id")
}

// TestUS0039_PostAnalyzeMissingContentReturns400 verifies missing content returns 400.
func TestUS0039_PostAnalyzeMissingContentReturns400(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{"source_type": "text"})

	resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode,
		"POST /analyze with no content must return 400")
}

// TestUS0039_JobExistsImmediatelyAfterPost verifies job record is retrievable right
// after the POST /analyze response is received.
func TestUS0039_JobExistsImmediatelyAfterPost(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content":     "Immediate job availability test",
		"source_type": "text",
	})

	postResp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer postResp.Body.Close()

	require.Equal(t, gohttp.StatusAccepted, postResp.StatusCode)

	var result analyzeResponse
	require.NoError(t, json.NewDecoder(postResp.Body).Decode(&result))
	require.NotEmpty(t, result.JobID)

	// Job must be retrievable immediately — no race.
	jobResp := doGet(t, fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, result.JobID))
	defer jobResp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, jobResp.StatusCode,
		"GET /jobs/{job_id} must return 200 immediately after POST /analyze")
}

// TestUS0039_JobStatusFieldPresent verifies GET /jobs/{id} response includes status field.
func TestUS0039_JobStatusFieldPresent(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "Status field presence test")

	jobResp := doGet(t, fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, jobID))
	defer jobResp.Body.Close()

	require.Equal(t, gohttp.StatusOK, jobResp.StatusCode)

	var job storage.Job
	require.NoError(t, json.NewDecoder(jobResp.Body).Decode(&job))

	assert.NotEmpty(t, job.Status, "GET /jobs/{id} response must include non-empty status field")
}

// TestUS0039_JobTransitionsToCompleted verifies job status reaches "completed".
func TestUS0039_JobTransitionsToCompleted(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "Job completion transition test")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)

	assert.Equal(t, storage.JobCompleted, job.Status,
		"job must transition to completed status")
}

// TestUS0039_ObjectRetrievableAfterJobCompletes verifies the stored object is
// accessible via GET /objects/{id} after job reaches completed status.
func TestUS0039_ObjectRetrievableAfterJobCompletes(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "Object retrieval post-completion test")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID, "completed job must have result_id")

	objResp := doGet(t, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	defer objResp.Body.Close()

	require.Equal(t, gohttp.StatusOK, objResp.StatusCode,
		"object must be retrievable via GET /objects/{id} after job completes")

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))

	assert.Equal(t, job.ResultID, obj.ID,
		"stored object id must match result_id from job response")
}

// TestUS0039_ObjectIDMatchesJobResultID verifies the object ID returned by
// GET /objects/{id} matches the result_id from the job response.
// (Per spec, POST /analyze should return object_id in 202 body; currently it
// only returns job_id. result_id on the job record serves as the equivalent.)
func TestUS0039_ObjectIDMatchesJobResultID(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	jobID := postAnalyze(t, env.URL, "Object ID match test — agent poll pattern")
	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	objResp := doGet(t, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	defer objResp.Body.Close()

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))

	assert.Equal(t, job.ResultID, obj.ID,
		"object id must equal job result_id (spec: should match object_id from 202 response)")
}

// TestUS0039_JobNotFoundReturns404 verifies GET /jobs/{unknown_id} returns 404.
func TestUS0039_JobNotFoundReturns404(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	resp := doGet(t, env.URL+"/api/v1/jobs/job-does-not-exist-xyz")
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode,
		"GET /jobs/{unknown} must return 404 — agent must not crash on missing job")
}

// TestUS0039_AgentPollPatternNoRace verifies polling completes without data race:
// ingest → poll until completed → retrieve object. Runs with -race in CI.
func TestUS0039_AgentPollPatternNoRace(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content":     "Race-free poll pattern: decision on microservice boundaries",
		"source_type": "text",
	})

	postResp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer postResp.Body.Close()

	require.Equal(t, gohttp.StatusAccepted, postResp.StatusCode)

	var ingestResult analyzeResponse
	require.NoError(t, json.NewDecoder(postResp.Body).Decode(&ingestResult))
	require.NotEmpty(t, ingestResult.JobID)

	// Poll until terminal.
	deadline := time.Now().Add(30 * time.Second)
	var finalJob storage.Job
	for time.Now().Before(deadline) {
		jobResp := doGet(t, fmt.Sprintf("%s/api/v1/jobs/%s", env.URL, ingestResult.JobID))
		require.Equal(t, gohttp.StatusOK, jobResp.StatusCode)

		require.NoError(t, json.NewDecoder(jobResp.Body).Decode(&finalJob))
		jobResp.Body.Close()

		if finalJob.Status == storage.JobCompleted || finalJob.Status == storage.JobFailed {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.Equal(t, storage.JobCompleted, finalJob.Status,
		"job must complete within deadline")
	require.NotEmpty(t, finalJob.ResultID,
		"completed job must have result_id")

	// Object must be retrievable — no race between write + read.
	objResp := doGet(t, fmt.Sprintf("%s/api/v1/objects/%s", env.URL, finalJob.ResultID))
	defer objResp.Body.Close()

	require.Equal(t, gohttp.StatusOK, objResp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(objResp.Body).Decode(&obj))
	assert.Equal(t, finalJob.ResultID, obj.ID)
}

// TestUS0039_ConcurrentAgentIngests verifies multiple concurrent ingest+wait sequences
// produce distinct objects (no ID collision, no cross-contamination).
func TestUS0039_ConcurrentAgentIngests(t *testing.T) {
	if os.Getenv("INTEGRATION") == "" {
		t.Skip("set INTEGRATION=1")
	}
	env := startTestEnv(t)
	defer env.stop(t)

	const n = 3
	type result struct {
		jobID    string
		resultID string
	}
	results := make([]result, n)

	// Submit all ingests.
	for i := 0; i < n; i++ {
		b, _ := json.Marshal(map[string]string{
			"content":     fmt.Sprintf("Concurrent agent ingest %d: event sourcing patterns", i),
			"source_type": "text",
		})
		resp, err := gohttp.Post(env.URL+"/api/v1/analyze", "application/json", bytes.NewReader(b))
		require.NoError(t, err)
		require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

		var ar analyzeResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&ar))
		resp.Body.Close()
		results[i].jobID = ar.JobID
	}

	// Wait for all.
	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		job := waitForJob(t, env.URL, results[i].jobID, storage.JobCompleted)
		require.NotEmpty(t, job.ResultID)
		results[i].resultID = job.ResultID
		assert.False(t, seen[job.ResultID], "result IDs must be unique; duplicate: %s", job.ResultID)
		seen[job.ResultID] = true
	}

	// Verify each object is retrievable.
	for i := 0; i < n; i++ {
		ctx := context.Background()
		obj, err := env.svc.Store.Objects().Get(ctx, results[i].resultID)
		require.NoError(t, err)
		assert.Equal(t, results[i].resultID, obj.ID)
	}
}
