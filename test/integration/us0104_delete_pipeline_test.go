package integration

import (
	"bytes"
	"encoding/json"
	"io"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestUS0104_DeleteCustom(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "delete-pipeline",
		"steps": stepsJSON,
	})
	respCreate, _ := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	respCreate.Body.Close()

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/delete-pipeline", nil)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusNoContent, resp.StatusCode)

	respGet, _ := gohttp.Get(env.URL + "/api/v1/pipelines/delete-pipeline")
	assert.Equal(t, gohttp.StatusNotFound, respGet.StatusCode)
}

func TestUS0104_DeleteBuiltIn(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/text.short", nil)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusForbidden, resp.StatusCode)
}

func TestUS0104_DeleteNonExistent(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/nonexistent", nil)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode)
}

func TestUS0104_DeleteConfirmedGoneFromList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "gone-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	respBefore, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respBefore.Body.Close()

	var beforeBody struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
	}
	require.NoError(t, json.NewDecoder(respBefore.Body).Decode(&beforeBody))

	namesBefore := make([]string, len(beforeBody.Pipelines))
	for i, p := range beforeBody.Pipelines {
		namesBefore[i] = p.Name
	}
	assert.Contains(t, namesBefore, "gone-pl")

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/gone-pl", nil)
	respDel, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer respDel.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, respDel.StatusCode)

	respAfter, err := gohttp.Get(env.URL + "/api/v1/pipelines")
	require.NoError(t, err)
	defer respAfter.Body.Close()

	var afterBody struct {
		Pipelines []storage.Pipeline `json:"pipelines"`
	}
	require.NoError(t, json.NewDecoder(respAfter.Body).Decode(&afterBody))

	namesAfter := make([]string, len(afterBody.Pipelines))
	for i, p := range afterBody.Pipelines {
		namesAfter[i] = p.Name
	}
	assert.NotContains(t, namesAfter, "gone-pl")
}

func TestUS0104_DeleteThenRecreate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "recycle-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/recycle-pl", nil)
	respDel, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	respDel.Body.Close()
	require.Equal(t, gohttp.StatusNoContent, respDel.StatusCode)

	body2, _ := json.Marshal(map[string]string{
		"name":  "recycle-pl",
		"steps": stepsJSON,
	})

	respRecreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body2))
	require.NoError(t, err)
	defer respRecreate.Body.Close()
	assert.Equal(t, gohttp.StatusCreated, respRecreate.StatusCode)
}

func TestUS0104_DeleteReturnsNoBody(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`
	body, _ := json.Marshal(map[string]string{
		"name":  "nobody-pl",
		"steps": stepsJSON,
	})

	respCreate, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	respCreate.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respCreate.StatusCode)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/nobody-pl", nil)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusNoContent, resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Empty(t, respBody)
}

func TestUS0104_DeleteDoesNotAffectOthers(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	stepsJSON := `[{"type":"text_summary"}]`

	bodyKeep, _ := json.Marshal(map[string]string{
		"name":  "keep-pl",
		"steps": stepsJSON,
	})
	respKeep, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(bodyKeep))
	require.NoError(t, err)
	respKeep.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respKeep.StatusCode)

	bodyRemove, _ := json.Marshal(map[string]string{
		"name":  "remove-pl",
		"steps": stepsJSON,
	})
	respRemove, err := gohttp.Post(env.URL+"/api/v1/pipelines", "application/json", bytes.NewReader(bodyRemove))
	require.NoError(t, err)
	respRemove.Body.Close()
	require.Equal(t, gohttp.StatusCreated, respRemove.StatusCode)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/pipelines/remove-pl", nil)
	respDel, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	respDel.Body.Close()
	require.Equal(t, gohttp.StatusNoContent, respDel.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/pipelines/keep-pl")
	require.NoError(t, err)
	defer respGet.Body.Close()
	assert.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	var pipeline storage.Pipeline
	require.NoError(t, json.NewDecoder(respGet.Body).Decode(&pipeline))
	assert.Equal(t, "keep-pl", pipeline.Name)
}
