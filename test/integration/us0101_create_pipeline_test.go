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

func TestUS0101_CreateFromJSON(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"noop"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "json-pipeline",
		"description": "JSON pipeline",
		"steps":       stepsJSON,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	assert.NotEmpty(t, result["id"])
}

func TestUS0101_InvalidStepsJSON(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"name":  "bad-pipeline",
		"steps": "invalid json",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0101_SandboxConfigStored(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"noop"}]`
	sandbox := map[string]any{
		"enabled":         true,
		"isolation_level": "process",
		"resource_limits": map[string]string{
			"max_memory": "512MB",
			"max_cpu":    "100%",
			"timeout":    "30s",
		},
		"network": false,
	}

	body, _ := json.Marshal(map[string]any{
		"name":        "sandbox-pipeline",
		"description": "With sandbox",
		"steps":       stepsJSON,
		"sandbox":     sandbox,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/sandbox-pipeline")
	require.NoError(t, err)
	defer respGet.Body.Close()

	var pipeline storage.Pipeline
	json.NewDecoder(respGet.Body).Decode(&pipeline)
	assert.NotNil(t, pipeline.Sandbox)
	assert.True(t, pipeline.Sandbox.Enabled)
	assert.Equal(t, "process", pipeline.Sandbox.IsolationLevel)
}

func TestUS0101_DuplicateName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"noop"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "dup-pipeline",
		"description": "First creation",
		"steps":       stepsJSON,
	})

	resp1, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp1.Body.Close()
	require.Equal(t, gohttp.StatusCreated, resp1.StatusCode)

	body2, _ := json.Marshal(map[string]string{
		"name":        "dup-pipeline",
		"description": "Duplicate creation",
		"steps":       stepsJSON,
	})

	resp2, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body2))
	require.NoError(t, err)
	defer resp2.Body.Close()

	assert.Equal(t, gohttp.StatusConflict, resp2.StatusCode, "duplicate name is a conflict")
	var env2 struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&env2))
	assert.Equal(t, "DUPLICATE_NAME", env2.Error.Code)
}

func TestUS0101_MissingName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"steps": `[{"type":"noop"}]`,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0101_MissingSteps(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"name": "no-steps-pl",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0101_MultipleSteps(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"noop"},{"type":"entity_extractor","config":{"threshold":0.5}}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "multi-step-pl",
		"steps": stepsJSON,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/multi-step-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))

	require.Len(t, pipeline.Steps, 2)
	assert.Equal(t, "noop", pipeline.Steps[0].Name)
	assert.Equal(t, "entity_extractor", pipeline.Steps[1].Name)
	assert.NotNil(t, pipeline.Steps[1].Config)
}

func TestUS0101_FullMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"noop"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "metadata-pl",
		"description": "A detailed description",
		"steps":       stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/metadata-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))

	assert.Equal(t, "metadata-pl", pipeline.Name)
	assert.Equal(t, "A detailed description", pipeline.Description)
	assert.False(t, pipeline.IsBuiltIn)
	assert.False(t, pipeline.Archived)
	assert.NotEmpty(t, pipeline.ID)
	assert.False(t, pipeline.CreatedAt.IsZero())
	assert.False(t, pipeline.UpdatedAt.IsZero())
}

func TestUS0101_StepNamesCaseSensitive(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// An upper-cased step name is a different, unregistered step: refused.
	bodyUpper, _ := json.Marshal(map[string]string{
		"name":  "case-upper",
		"steps": `[{"type":"Noop"}]`,
	})
	respUpper, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(bodyUpper))
	require.NoError(t, err)
	defer respUpper.Body.Close()
	require.Equal(t, gohttp.StatusBadRequest, respUpper.StatusCode)

	// Create pipeline with lower-cased step name.
	bodyLower, _ := json.Marshal(map[string]string{
		"name":  "case-lower",
		"steps": `[{"type":"noop"}]`,
	})
	respLower, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(bodyLower))
	require.NoError(t, err)
	defer respLower.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respLower.StatusCode)

	// GET and verify casing is preserved for lower-case pipeline.
	getLower, err := gohttp.Get(env.URL + "/api/v1/pipelines/case-lower")
	require.NoError(t, err)
	defer getLower.Body.Close()
	require.Equal(t, gohttp.StatusOK, getLower.StatusCode)

	var pipelineLower storage.Pipeline
	require.NoError(t, json.NewDecoder(getLower.Body).Decode(&pipelineLower))
	require.Len(t, pipelineLower.Steps, 1)
	assert.Equal(t, "noop", pipelineLower.Steps[0].Name, "step name casing should be preserved")
}

func TestUS0101_PipelineImmediatelyAvailableForEnqueue(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	// Create a pipeline.
	createBody, _ := json.Marshal(map[string]string{
		"name":  "enqueue-ready",
		"steps": `[{"type":"noop"}]`,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(createBody))
	require.NoError(t, err)
	defer respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	// Immediately enqueue content to the just-created pipeline.
	enqueueBody, _ := json.Marshal(map[string]string{
		"content":  "test content for enqueue",
		"type":     "text",
		"source":   "test",
		"pipeline": "enqueue-ready",
	})
	respEnqueue, err := gohttp.Post(env.URL+"/api/v1/pipelines/enqueue", "application/json", bytes.NewReader(enqueueBody))
	require.NoError(t, err)
	defer respEnqueue.Body.Close()

	assert.Equal(t, gohttp.StatusAccepted, respEnqueue.StatusCode)

	var result map[string]string
	require.NoError(t, json.NewDecoder(respEnqueue.Body).Decode(&result))
	assert.NotEmpty(t, result["job_id"], "enqueue should return a job_id")
}

func TestUS0101_EmptyStepsArray(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"name":  "empty-steps-pl",
		"steps": "[]",
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	// The server may accept an empty steps array or reject it; either is valid.
	assert.Contains(t,
		[]int{gohttp.StatusCreated, gohttp.StatusBadRequest},
		resp.StatusCode,
		"empty steps array should return 201 (accepted) or 400 (rejected), got %d", resp.StatusCode,
	)
}

func TestUS0101_UnknownStepAndProtectedName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	post := func(name, steps string) *gohttp.Response {
		body, _ := json.Marshal(map[string]string{"name": name, "steps": steps})
		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}

	assert.Equal(t, gohttp.StatusBadRequest, post("legal-doc-pipeline", `[{"type":"legal_classifier"}]`).StatusCode,
		"unregistered step is a validation error")
	assert.Equal(t, gohttp.StatusForbidden, post("text.mine", `[{"type":"noop"}]`).StatusCode,
		"text.* names are protected")
}
