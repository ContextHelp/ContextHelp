package integration

import (
	"encoding/json"
	gohttp "net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestUS0109_RegistryListEmpty(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/registries")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Registries []storage.RegistryCache `json:"registries"`
		Total      int                     `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, 0, body.Total)
}

func TestUS0109_CacheThenList(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	ctx := t
	_ = ctx
	cache := &storage.RegistryCache{
		RegistryURL: "https://example.com/steps",
		Manifest: &storage.RegistryManifest{
			Name:        "example-registry",
			Description: "An example step registry",
			Version:     "1.0",
			Steps: []storage.ManifestStep{
				{Name: "step-a", Path: "/steps/a", License: "MIT", Version: "1.0"},
				{Name: "step-b", Path: "/steps/b", License: "Apache-2.0", Version: "2.0"},
			},
		},
		LastFetched: time.Now(),
		ETag:        "etag-123",
		AutoUpdate:  false,
	}
	err := env.svc.Store.Registries().CacheManifest(t.Context(), cache)
	require.NoError(t, err)

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/registries")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Registries []storage.RegistryCache `json:"registries"`
		Total      int                     `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	assert.GreaterOrEqual(t, body.Total, 1)

	var found *storage.RegistryCache
	for i := range body.Registries {
		if body.Registries[i].RegistryURL == "https://example.com/steps" {
			found = &body.Registries[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "etag-123", found.ETag)
	assert.NotNil(t, found.Manifest)
	assert.Equal(t, "example-registry", found.Manifest.Name)
}

func TestUS0109_CacheManifestFieldsPreserved(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	cache := &storage.RegistryCache{
		RegistryURL: "https://fields-test.example.com/steps",
		Manifest: &storage.RegistryManifest{
			Name:        "fields-registry",
			Description: "Registry for field preservation test",
			Version:     "2.5",
			Steps: []storage.ManifestStep{
				{Name: "step-x", Path: "/steps/x", License: "MIT", Version: "1.0"},
				{Name: "step-y", Path: "/steps/y", License: "BSD-3-Clause", Version: "3.1"},
			},
		},
		LastFetched: time.Now(),
		ETag:        "etag-fields",
		AutoUpdate:  true,
	}
	err := env.svc.Store.Registries().CacheManifest(t.Context(), cache)
	require.NoError(t, err)

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/registries")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Registries []storage.RegistryCache `json:"registries"`
		Total      int                     `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))

	var found *storage.RegistryCache
	for i := range body.Registries {
		if body.Registries[i].RegistryURL == "https://fields-test.example.com/steps" {
			found = &body.Registries[i]
			break
		}
	}
	require.NotNil(t, found, "cached registry should appear in list")
	require.NotNil(t, found.Manifest)

	assert.Equal(t, "fields-registry", found.Manifest.Name)
	assert.Equal(t, "Registry for field preservation test", found.Manifest.Description)
	assert.Equal(t, "2.5", found.Manifest.Version)
	require.Len(t, found.Manifest.Steps, 2)

	assert.Equal(t, "step-x", found.Manifest.Steps[0].Name)
	assert.Equal(t, "/steps/x", found.Manifest.Steps[0].Path)
	assert.Equal(t, "MIT", found.Manifest.Steps[0].License)
	assert.Equal(t, "1.0", found.Manifest.Steps[0].Version)

	assert.Equal(t, "step-y", found.Manifest.Steps[1].Name)
	assert.Equal(t, "/steps/y", found.Manifest.Steps[1].Path)
	assert.Equal(t, "BSD-3-Clause", found.Manifest.Steps[1].License)
	assert.Equal(t, "3.1", found.Manifest.Steps[1].Version)
}

func TestUS0109_MultipleCaches(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	cache1 := &storage.RegistryCache{
		RegistryURL: "https://multi-a.example.com/steps",
		Manifest: &storage.RegistryManifest{
			Name:    "multi-registry-a",
			Version: "1.0",
			Steps:   []storage.ManifestStep{{Name: "step-a1", Path: "/a1", License: "MIT", Version: "1.0"}},
		},
		LastFetched: time.Now(),
		ETag:        "etag-a",
	}
	cache2 := &storage.RegistryCache{
		RegistryURL: "https://multi-b.example.com/steps",
		Manifest: &storage.RegistryManifest{
			Name:    "multi-registry-b",
			Version: "2.0",
			Steps:   []storage.ManifestStep{{Name: "step-b1", Path: "/b1", License: "Apache-2.0", Version: "2.0"}},
		},
		LastFetched: time.Now(),
		ETag:        "etag-b",
	}

	ctx := t.Context()
	require.NoError(t, env.svc.Store.Registries().CacheManifest(ctx, cache1))
	require.NoError(t, env.svc.Store.Registries().CacheManifest(ctx, cache2))

	resp, err := gohttp.Get(env.URL + "/api/v1/steps/registries")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var body struct {
		Registries []storage.RegistryCache `json:"registries"`
		Total      int                     `json:"total"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.GreaterOrEqual(t, body.Total, 2)

	urls := make([]string, len(body.Registries))
	for i, r := range body.Registries {
		urls[i] = r.RegistryURL
	}
	assert.Contains(t, urls, "https://multi-a.example.com/steps")
	assert.Contains(t, urls, "https://multi-b.example.com/steps")
}
