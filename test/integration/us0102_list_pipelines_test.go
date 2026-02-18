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

func TestUS0102_ListNonArchived(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	createTestPipeline := func(name string) {
		stepsJSON := `[{"type":"text_summary"}]`
		body, _ := json.Marshal(map[string]string{
			"name":  name,
			"steps": stepsJSON,
		})
		resp, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		resp.Body.Close()
	}

	createTestPipeline("custom-pipeline-1")
	createTestPipeline("custom-pipeline-2")

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	assert.GreaterOrEqual(t, len(body.Pipelines), 2)
}

func TestUS0102_IncludeArchived(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines?include_archived=true")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)
}

func TestUS0102_NameFilter(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "filter-target",
		"steps": stepsJSON,
	})
	resp, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	resp.Body.Close()

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines?name=filter-target")
	require.NoError(t, err)
	defer respList.Body.Close()

	assert.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	json.NewDecoder(respList.Body).Decode(&result)

	if result.Total > 0 {
		assert.Equal(t, "filter-target", result.Pipelines[0].Name)
	}
}

func TestUS0102_OnlyArchived(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`

	for _, name := range []string{"active-pl", "archived-pl"} {
		body, _ := json.Marshal(map[string]string{
			"name":  name,
			"steps": stepsJSON,
		})
		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	}

	respArchive, err := gohttp.Post(env.URL+"/api/v1/pipelines/archived-pl/archive", "application/json", nil)
	require.NoError(t, err)
	defer respArchive.Body.Close()
	require.Equal(t, gohttp.StatusOK, respArchive.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines?only_archived=true")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&result))

	names := make([]string, len(result.Pipelines))
	for i, p := range result.Pipelines {
		names[i] = p.Name
	}

	assert.Contains(t, names, "archived-pl")
	assert.NotContains(t, names, "active-pl")
}

func TestUS0102_IncludeArchivedShowsBoth(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`

	for _, name := range []string{"inc-active", "inc-archived"} {
		body, _ := json.Marshal(map[string]string{
			"name":  name,
			"steps": stepsJSON,
		})
		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	}

	respArchive, err := gohttp.Post(env.URL+"/api/v1/pipelines/inc-archived/archive", "application/json", nil)
	require.NoError(t, err)
	defer respArchive.Body.Close()
	require.Equal(t, gohttp.StatusOK, respArchive.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines?include_archived=true")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&result))

	names := make([]string, len(result.Pipelines))
	for i, p := range result.Pipelines {
		names[i] = p.Name
	}

	assert.Contains(t, names, "inc-active")
	assert.Contains(t, names, "inc-archived")
}

func TestUS0102_ExcludesArchived(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`

	for _, name := range []string{"visible-pl", "hidden-pl"} {
		body, _ := json.Marshal(map[string]string{
			"name":  name,
			"steps": stepsJSON,
		})
		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	}

	respArchive, err := gohttp.Post(env.URL+"/api/v1/pipelines/hidden-pl/archive", "application/json", nil)
	require.NoError(t, err)
	defer respArchive.Body.Close()
	require.Equal(t, gohttp.StatusOK, respArchive.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&result))

	names := make([]string, len(result.Pipelines))
	for i, p := range result.Pipelines {
		names[i] = p.Name
	}

	assert.Contains(t, names, "visible-pl")
	assert.NotContains(t, names, "hidden-pl")
}

func TestUS0102_StepCountAccurate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"},{"type":"entity_extract"},{"type":"tag_assign"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "three-step-pipeline",
		"steps": stepsJSON,
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, gohttp.StatusCreated, resp.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&result))

	var found bool
	for _, p := range result.Pipelines {
		if p.Name == "three-step-pipeline" {
			found = true
			assert.Equal(t, 3, len(p.Steps))
			break
		}
	}
	assert.True(t, found, "expected to find pipeline 'three-step-pipeline' in list")
}

func TestUS0102_JSONResponseValid(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var raw map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&raw), "response body must be valid JSON")

	pipelinesVal, ok := raw["pipelines"]
	require.True(t, ok, "response must contain 'pipelines' key")
	// When no pipelines exist, JSON encodes nil slice as null, which is valid.
	if pipelinesVal != nil {
		_, ok = pipelinesVal.([]any)
		assert.True(t, ok, "'pipelines' must be an array or null")
	}

	totalVal, ok := raw["total"]
	require.True(t, ok, "response must contain 'total' key")
	_, ok = totalVal.(float64)
	assert.True(t, ok, "'total' must be a number")
}

func TestUS0102_TotalMatchesLength(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	for _, name := range []string{"tm-pipeline-1", "tm-pipeline-2", "tm-pipeline-3"} {
		body, _ := json.Marshal(map[string]string{
			"name":  name,
			"steps": stepsJSON,
		})
		resp, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		resp.Body.Close()
		require.Equal(t, gohttp.StatusCreated, resp.StatusCode)
	}

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&result))

	assert.Equal(t, result.Total, len(result.Pipelines))
	assert.GreaterOrEqual(t, result.Total, 3)
}

func TestUS0102_EmptyListReturnsZero(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var result struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	assert.Equal(t, result.Total, len(result.Pipelines))
}
