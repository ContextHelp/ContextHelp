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

func TestUS0105_ArchiveSetsFlag(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "archive-pipeline",
		"steps": stepsJSON,
	})
	respCreate, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	respCreate.Body.Close()

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/archive-pipeline/archive", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	respGet, _ := gohttp.Get(env.URL + "/api/v1/pipelines/archive-pipeline")
	var pipeline storage.Pipeline
	json.NewDecoder(respGet.Body).Decode(&pipeline)
	assert.True(t, pipeline.Archived)
}

func TestUS0105_UnarchiveMakesAvailable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "unarch-pipeline",
		"steps": stepsJSON,
	})
	respCreate, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	respCreate.Body.Close()

	gohttp.Post(env.URL+"/api/v1/pipelines/unarch-pipeline/archive", "application/json", nil)

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/unarch-pipeline/unarchive", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	respGet, _ := gohttp.Get(env.URL + "/api/v1/pipelines/unarch-pipeline")
	var pipeline storage.Pipeline
	json.NewDecoder(respGet.Body).Decode(&pipeline)
	assert.False(t, pipeline.Archived)
}

func TestUS0105_ArchiveIdempotent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "idem-pl",
		"steps": stepsJSON,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()

	resp1, err := gohttp.Post(env.URL+"/api/v1/pipelines/idem-pl/archive", "application/json", nil)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp1.StatusCode)

	resp2, err := gohttp.Post(env.URL+"/api/v1/pipelines/idem-pl/archive", "application/json", nil)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp2.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/idem-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	assert.True(t, pipeline.Archived)
}

func TestUS0105_ArchiveExcludesFromDefaultList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "arch-excl-pl",
		"steps": stepsJSON,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()

	resp, err := gohttp.Post(env.URL+"/api/v1/pipelines/arch-excl-pl/archive", "application/json", nil)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	respList, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var listBody struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
		Total     int                `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&listBody))

	for _, p := range listBody.Pipelines {
		assert.NotEqual(t, "arch-excl-pl", p.Name, "archived pipeline should not appear in default list")
	}
}

func TestUS0105_UnarchiveIdempotent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "unarch-idem-pl",
		"steps": stepsJSON,
	})
	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()

	respArchive, err := gohttp.Post(env.URL+"/api/v1/pipelines/unarch-idem-pl/archive", "application/json", nil)
	require.NoError(t, err)
	respArchive.Body.Close()

	resp1, err := gohttp.Post(env.URL+"/api/v1/pipelines/unarch-idem-pl/unarchive", "application/json", nil)
	require.NoError(t, err)
	defer resp1.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp1.StatusCode)

	resp2, err := gohttp.Post(env.URL+"/api/v1/pipelines/unarch-idem-pl/unarchive", "application/json", nil)
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, gohttp.StatusOK, resp2.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/unarch-idem-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	assert.False(t, pipeline.Archived)
}
