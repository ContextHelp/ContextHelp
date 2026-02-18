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

func TestUS0107_ListAvailableSteps(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	if err := env.svc.Discovery.DiscoverAll(ctx); err != nil {
		t.Fatalf("discovery: %v", err)
	}

	resp, err := gohttp.Get(env.URL + "/api/v1/steps")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Steps []storage.RegisteredStep `json:"steps"`
		Total int                      `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.GreaterOrEqual(t, len(body.Steps), 3)
}

func TestUS0107_GetByName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name:   "test-extractor",
		Source: "builtin",
		Path:   "",
		Metadata: &storage.StepMetadata{
			Name:        "test-extractor",
			Description: "A test extractor step",
			Version:     "2.1.0",
		},
		InstalledAt: now,
		UpdatedAt:   now,
	}
	err := env.svc.Store.Steps().Create(ctx, step)
	require.NoError(t, err)

	t.Run("existing step", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/steps/test-extractor")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

		var got storage.RegisteredStep
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
		assert.Equal(t, "test-extractor", got.Name)
		assert.Equal(t, "builtin", got.Source)
		require.NotNil(t, got.Metadata)
		assert.Equal(t, "2.1.0", got.Metadata.Version)
	})

	t.Run("nonexistent returns 404", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/steps/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode)

		var errBody struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&errBody))
		assert.Equal(t, "NOT_FOUND", errBody.Error.Code)
	})
}

func TestUS0107_FilterBySource(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()

	builtinStep := &storage.RegisteredStep{
		Name: "filter-builtin-step", Source: "builtin", Path: "",
		Metadata:    &storage.StepMetadata{Name: "filter-builtin-step", Description: "A builtin step"},
		InstalledAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, builtinStep))

	localStep := &storage.RegisteredStep{
		Name: "filter-local-step", Source: "local:/tmp/custom", Path: "/tmp/custom/step",
		Metadata:    &storage.StepMetadata{Name: "filter-local-step", Description: "A local step"},
		InstalledAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, localStep))

	t.Run("filter by builtin", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/steps?source=builtin")
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, gohttp.StatusOK, resp.StatusCode)

		var body struct {
			Steps []storage.RegisteredStep `json:"steps"`
			Total int                      `json:"total"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, 1, body.Total)
		if len(body.Steps) > 0 {
			assert.Equal(t, "builtin", body.Steps[0].Source)
		}
	})

	t.Run("no filter returns all", func(t *testing.T) {
		resp, err := gohttp.Get(env.URL + "/api/v1/steps")
		require.NoError(t, err)
		defer resp.Body.Close()

		var body struct {
			Steps []storage.RegisteredStep `json:"steps"`
			Total int                      `json:"total"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.GreaterOrEqual(t, body.Total, 2)
	})
}

func TestUS0107_MetadataAllFields(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name: "full-meta-step", Source: "builtin", Path: "/steps/full",
		Metadata: &storage.StepMetadata{
			Name: "full-meta-step", Description: "Complete metadata test",
			License: "Apache-2.0", Version: "3.2.1", Author: "testbot",
		},
		InstalledAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/full-meta-step")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var got storage.RegisteredStep
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))

	assert.Equal(t, "full-meta-step", got.Name)
	assert.Equal(t, "builtin", got.Source)
	assert.Equal(t, "/steps/full", got.Path)
	require.NotNil(t, got.Metadata)
	assert.Equal(t, "Complete metadata test", got.Metadata.Description)
	assert.Equal(t, "Apache-2.0", got.Metadata.License)
	assert.Equal(t, "3.2.1", got.Metadata.Version)
	assert.Equal(t, "testbot", got.Metadata.Author)
}

func TestUS0107_UpdateMetadata(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name: "updatable-step", Source: "builtin", Path: "",
		Metadata:    &storage.StepMetadata{Name: "updatable-step", Description: "Updatable", Version: "1.0.0"},
		InstalledAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/updatable-step")
	require.NoError(t, err)
	defer resp.Body.Close()

	var got storage.RegisteredStep
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	assert.Equal(t, "1.0.0", got.Metadata.Version)

	step.Metadata.Version = "2.0.0"
	step.UpdatedAt = time.Now()
	require.NoError(t, env.svc.Store.Steps().Update(ctx, step))

	resp2, err := gohttp.Get(env.URL + "/api/v1/steps/updatable-step")
	require.NoError(t, err)
	defer resp2.Body.Close()

	var got2 storage.RegisteredStep
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&got2))
	assert.Equal(t, "2.0.0", got2.Metadata.Version)
}

func TestUS0107_Uninstall(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := context.Background()
	now := time.Now()
	step := &storage.RegisteredStep{
		Name: "removable-step", Source: "local:/tmp/test", Path: "/tmp/test/step",
		Metadata:    &storage.StepMetadata{Name: "removable-step", Description: "Will be uninstalled"},
		InstalledAt: now, UpdatedAt: now,
	}
	require.NoError(t, env.svc.Store.Steps().Create(ctx, step))

	respGet, err := gohttp.Get(env.URL + "/api/v1/steps/removable-step")
	require.NoError(t, err)
	defer respGet.Body.Close()
	require.Equal(t, gohttp.StatusOK, respGet.StatusCode)

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/steps/removable-step", nil)
	resp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, resp.StatusCode)

	respGone, err := gohttp.Get(env.URL + "/api/v1/steps/removable-step")
	require.NoError(t, err)
	defer respGone.Body.Close()
	assert.Equal(t, gohttp.StatusNotFound, respGone.StatusCode)
}
