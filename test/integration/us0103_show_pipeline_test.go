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

func TestUS0103_ShowExisting(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "show-pipeline",
		"description": "For testing",
		"steps":       stepsJSON,
	})
	respCreate, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	respCreate.Body.Close()

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines/show-pipeline")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var pipeline storage.Pipeline
	json.NewDecoder(resp.Body).Decode(&pipeline)
	assert.Equal(t, "show-pipeline", pipeline.Name)
}

func TestUS0103_ShowNonExistent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines/nonexistent")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode)
}

func TestUS0103_ShowArchived(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "show-archived-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respArchive, err := gohttp.Post(env.URL+"/api/v1/pipelines/show-archived-pl/archive", "application/json", nil)
	require.NoError(t, err)
	respArchive.Body.Close()
	require.Equal(t, gohttp.StatusOK, respArchive.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/show-archived-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	assert.Equal(t, "show-archived-pl", pipeline.Name)
	assert.True(t, pipeline.Archived)
}

func TestUS0103_ShowStepsWithConfigs(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary","config":{"max_length":100}}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "steps-config-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/steps-config-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	require.NotEmpty(t, pipeline.Steps)
	assert.Equal(t, "text_summary", pipeline.Steps[0].Name)
}

func TestUS0103_ResourceLimitsRoundTrip(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	sandbox := map[string]any{
		"enabled":         true,
		"isolation_level": "container",
		"resource_limits": map[string]string{
			"max_memory": "256MB",
			"max_cpu":    "1.5",
			"timeout":    "60s",
		},
		"network": true,
	}

	body, _ := json.Marshal(map[string]any{
		"name":    "limits-pl",
		"steps":   stepsJSON,
		"sandbox": sandbox,
	})

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/limits-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	require.NotNil(t, pipeline.Sandbox)
	assert.True(t, pipeline.Sandbox.Enabled)
	assert.Equal(t, "container", pipeline.Sandbox.IsolationLevel)
	assert.True(t, pipeline.Sandbox.Network)
	require.NotNil(t, pipeline.Sandbox.ResourceLimits)
	assert.Equal(t, "256MB", pipeline.Sandbox.ResourceLimits.MaxMemory)
	assert.Equal(t, "1.5", pipeline.Sandbox.ResourceLimits.MaxCPU)
	assert.Equal(t, "60s", pipeline.Sandbox.ResourceLimits.Timeout)
}

func TestUS0103_SandboxConfigComplete(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	sandbox := map[string]any{
		"enabled":         true,
		"isolation_level": "container",
		"resource_limits": map[string]string{
			"max_memory": "1GB",
			"max_cpu":    "2.0",
			"timeout":    "120s",
		},
		"network": false,
		"filesystem": map[string]any{
			"read_only":     true,
			"write_allowed": false,
		},
	}

	body, _ := json.Marshal(map[string]any{
		"name":    "sandbox-complete-pl",
		"steps":   stepsJSON,
		"sandbox": sandbox,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/sandbox-complete-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	require.NotNil(t, pipeline.Sandbox)
	assert.True(t, pipeline.Sandbox.Enabled)
	assert.Equal(t, "container", pipeline.Sandbox.IsolationLevel)
	assert.False(t, pipeline.Sandbox.Network)
	require.NotNil(t, pipeline.Sandbox.ResourceLimits)
	assert.Equal(t, "1GB", pipeline.Sandbox.ResourceLimits.MaxMemory)
	assert.Equal(t, "2.0", pipeline.Sandbox.ResourceLimits.MaxCPU)
	assert.Equal(t, "120s", pipeline.Sandbox.ResourceLimits.Timeout)
	require.NotNil(t, pipeline.Sandbox.Filesystem)
	assert.True(t, pipeline.Sandbox.Filesystem.ReadOnly)
	assert.False(t, pipeline.Sandbox.Filesystem.WriteAllowed)
}

func TestUS0103_MetadataAccurate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":        "metadata-pl",
		"description": "A pipeline for metadata accuracy testing",
		"steps":       stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/metadata-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	assert.Equal(t, "metadata-pl", pipeline.Name)
	assert.Equal(t, "A pipeline for metadata accuracy testing", pipeline.Description)
	assert.False(t, pipeline.IsBuiltIn)
	assert.False(t, pipeline.Archived)
	assert.False(t, pipeline.CreatedAt.IsZero())
	assert.False(t, pipeline.UpdatedAt.IsZero())
	assert.NotEmpty(t, pipeline.Steps)
}

func TestUS0103_ShowMultipleStepsPreservesOrder(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"},{"type":"entity_extraction"},{"type":"tagging"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "ordered-steps-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/ordered-steps-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	require.Len(t, pipeline.Steps, 3)
	assert.Equal(t, "text_summary", pipeline.Steps[0].Name)
	assert.Equal(t, "entity_extraction", pipeline.Steps[1].Name)
	assert.Equal(t, "tagging", pipeline.Steps[2].Name)
}

func TestUS0103_ShowReturnsJSONContentType(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "content-type-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/content-type-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	contentType := respGet.Header.Get("Content-Type")
	assert.Contains(t, contentType, "application/json")
}
