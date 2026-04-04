package integration

import (
	"bytes"
	"encoding/json"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestUS0106_EnqueueBasic(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "enqueue-pipeline",
		"steps": stepsJSON,
	})
	respCreate, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	respCreate.Body.Close()

	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "test content for enqueue",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "enqueue-pipeline",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result["job_id"])
}

func TestUS0106_JobHasCorrectPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "job-check-pipeline",
		"steps": stepsJSON,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()

	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "test content for job check",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "job-check-pipeline",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var enqueueResult map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&enqueueResult))
	jobID := enqueueResult["job_id"]
	require.NotEmpty(t, jobID)

	respJob, err := gohttp.Get(env.URL + "/api/v1/jobs/" + jobID)
	require.NoError(t, err)
	defer respJob.Body.Close()
	require.Equal(t, gohttp.StatusOK, respJob.StatusCode)

	var job storage.Job
	require.NoError(t, json.NewDecoder(respJob.Body).Decode(&job))
	assert.Equal(t, "job-check-pipeline", job.Pipeline)
}

func TestUS0106_AutoSelectsPipeline(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	enqueueBody, _ := json.Marshal(map[string]string{
		"content": "hello world",
		"type":    "text",
		"source":  "e2e-test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result["job_id"])

	jobResp, err := gohttp.Get(env.URL + "/api/v1/jobs/" + result["job_id"])
	require.NoError(t, err)
	defer jobResp.Body.Close()

	var job storage.Job
	require.NoError(t, json.NewDecoder(jobResp.Body).Decode(&job))
	assert.NotEmpty(t, job.Pipeline, "pipeline should be auto-selected when not specified")
}

func TestUS0106_MissingContentReturns400(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"type":   "text",
		"source": "test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0106_TypeDefaultsToText(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"content": "some text",
		"source":  "test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result["job_id"])

	jobResp, err := gohttp.Get(env.URL + "/api/v1/jobs/" + result["job_id"])
	require.NoError(t, err)
	defer jobResp.Body.Close()

	var job storage.Job
	require.NoError(t, json.NewDecoder(jobResp.Body).Decode(&job))
	assert.Contains(t, job.Type, "text")
}

func TestUS0106_JobTypeFormat(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	enqueueBody, _ := json.Marshal(map[string]string{
		"content": "test",
		"type":    "text",
		"source":  "e2e-test",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result["job_id"])

	jobResp, err := gohttp.Get(env.URL + "/api/v1/jobs/" + result["job_id"])
	require.NoError(t, err)
	defer jobResp.Body.Close()

	var job storage.Job
	require.NoError(t, json.NewDecoder(jobResp.Body).Decode(&job))
	assert.Equal(t, "ingest:text", job.Type)
}

func TestUS0106_MultipleJobsUniqueIDs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ids := make(map[string]bool)
	for i := 0; i < 3; i++ {
		body, _ := json.Marshal(map[string]string{
			"content": "unique content " + string(rune('A'+i)),
			"type":    "text",
			"source":  "e2e-test",
		})

		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

		var result map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
		jobID := result["job_id"]
		require.NotEmpty(t, jobID)
		ids[jobID] = true
	}

	assert.Len(t, ids, 3, "all 3 job IDs should be distinct")
}

func TestUS0106_CreateThenEnqueue(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "roundtrip-pipeline",
		"description": "Roundtrip test",
		"steps":       stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "roundtrip test content",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "roundtrip-pipeline",
	})

	respEnqueue, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer respEnqueue.Body.Close()
	require.Equal(t, gohttp.StatusAccepted, respEnqueue.StatusCode)

	var enqueueResult map[string]string
	require.NoError(t, json.NewDecoder(respEnqueue.Body).Decode(&enqueueResult))
	jobID := enqueueResult["job_id"]
	require.NotEmpty(t, jobID)

	respJob, err := gohttp.Get(env.URL + "/api/v1/jobs/" + jobID)
	require.NoError(t, err)
	defer respJob.Body.Close()

	var job storage.Job
	require.NoError(t, json.NewDecoder(respJob.Body).Decode(&job))
	assert.Equal(t, "roundtrip-pipeline", job.Pipeline)
}

func TestUS0106_EnqueueInvalidPipelineRejected(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "content for nonexistent pipeline",
		"type":     "text",
		"source":   "e2e-test",
		"pipeline": "nonexistent-pipeline-xyz",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	// With pipeline validation, unknown pipelines are rejected at enqueue time.
	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0106_EnqueueEmptySourceAccepted(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	enqueueBody, _ := json.Marshal(map[string]string{
		"content": "content without source",
		"type":    "text",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, resp.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"])
}
