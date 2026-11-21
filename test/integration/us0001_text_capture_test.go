package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

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

// Short text with no pipeline named is routed to the real text.short
// pipeline; the object carries type, pipeline and source.
func TestUS0001_JobCompletesWithCorrectTypeAndPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	jobID, err := env.svc.Analyze(ownerCtx(), service.AnalyzeRequest{
		Content: "Short text insight for testing",
		Type:    "text",
		Source:  "e2e-test",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)
	assert.Equal(t, "text.short", job.Pipeline)

	obj := getObject(t, env.URL, job.ResultID)
	assert.Equal(t, "text", obj.Type)
	assert.Equal(t, "text.short", obj.Pipeline)
	assert.Equal(t, "e2e-test", obj.Source)
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

// The profile named on the request is persisted on the object. (The story
// also names a project field; AnalyzeRequest has none, so it is not tested.)
func TestUS0001_ProfileAndProjectPersistedInMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	jobID, err := env.svc.Analyze(ownerCtx(), service.AnalyzeRequest{
		Content: "Insight with profile context",
		Type:    "text",
		Source:  "e2e-test",
		Profile: "founder",
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	obj := getObject(t, env.URL, job.ResultID)
	assert.Equal(t, "founder", obj.ProfileID)
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
