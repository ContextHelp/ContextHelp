package integration

import (
	"context"
	"encoding/json"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// US-0029: install pipeline steps from a registry. These cover the step
// store and the /api/v1/steps read and uninstall routes.

// TestUS0029_StepStoredAndRetrievable verifies that a step stored directly via
// the Steps store appears in GET /api/v1/steps/{name}.
// Note: The REST install endpoint (POST /api/v1/steps/install) downloads from a
// real registry URL — in tests we write directly to storage to verify the
// storage+API contract without requiring network access.
func TestUS0029_StepStoredAndRetrievable(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:   "extract-entities",
		Source: "registry:https://registry.example.com/steps",
		Path:   "",
		Metadata: &storage.StepMetadata{
			Name:        "extract-entities",
			Description: "Extracts named entities from text",
			Version:     "1.0.0",
		},
		InstalledAt: now,
		UpdatedAt:   now,
	}
	err := env.svc.Store.Steps().Create(ctx, step)
	require.NoError(t, err)

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/extract-entities")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var got storage.RegisteredStep
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	assert.Equal(t, "extract-entities", got.Name)
	assert.NotEmpty(t, got.Source, "source must be populated")
	assert.False(t, got.InstalledAt.IsZero(), "installed_at must be set")
}

// TestUS0029_InstalledStepAppearsInList verifies GET /api/v1/steps includes a
// step that was registered via the Steps store.
func TestUS0029_InstalledStepAppearsInList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:   "list-test-step",
		Source: "registry:https://registry.example.com/steps",
		Metadata: &storage.StepMetadata{
			Name:    "list-test-step",
			Version: "2.0.0",
		},
		InstalledAt: now,
		UpdatedAt:   now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	respList, err := gohttp.Get(env.URL + "/api/v1/steps")
	require.NoError(t, err)
	defer respList.Body.Close()
	require.Equal(t, gohttp.StatusOK, respList.StatusCode)

	var listBody struct {
		Steps []storage.RegisteredStep `json:"steps"`
		Total int                      `json:"total"`
	}
	require.NoError(t, json.NewDecoder(respList.Body).Decode(&listBody))

	found := false
	for _, s := range listBody.Steps {
		if s.Name == "list-test-step" {
			found = true
			break
		}
	}
	assert.True(t, found, "registered step must appear in GET /api/v1/steps")
}

// TestUS0029_UninstallStepRemovesIt verifies DELETE /api/v1/steps/{name} removes
// the step and subsequent GET returns 404.
func TestUS0029_UninstallStepRemovesIt(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:        "delete-test-step",
		Source:      "registry:https://registry.example.com/steps",
		InstalledAt: now,
		UpdatedAt:   now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/steps/delete-test-step", nil)
	respDel, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	respDel.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, respDel.StatusCode)

	respGet, err := gohttp.Get(env.URL + "/api/v1/steps/delete-test-step")
	require.NoError(t, err)
	defer respGet.Body.Close()
	assert.Equal(t, gohttp.StatusNotFound, respGet.StatusCode)
}
