package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// mockRegistryServer starts an httptest server that serves a registry manifest
// with the given steps and a SKILL.md for each step under /<path>.
func mockRegistryServer(t *testing.T, manifest storage.RegistryManifest) *httptest.Server {
	t.Helper()
	mux := gohttp.NewServeMux()

	// Serve the manifest at root.
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)

	mux.HandleFunc("/", func(w gohttp.ResponseWriter, r *gohttp.Request) {
		// Root path serves the manifest.
		if r.URL.Path == "/" || r.URL.Path == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(gohttp.StatusOK)
			w.Write(manifestJSON)
			return
		}
		// Step paths serve a minimal SKILL.md.
		for _, step := range manifest.Steps {
			if r.URL.Path == "/"+step.Path || r.URL.Path == step.Path {
				skillContent := fmt.Sprintf("---\nname: %s\nversion: %s\nlicense: MIT\ndescription: Mock step\n---\n",
					step.Name, step.Version)
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(gohttp.StatusOK)
				w.Write([]byte(skillContent))
				return
			}
		}
		gohttp.NotFound(w, r)
	})

	return httptest.NewServer(mux)
}

func TestUS0108_InstallStep_DownloadAndRegister(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	manifest := storage.RegistryManifest{
		Name:    "test-registry",
		Version: "1.0",
		Steps: []storage.ManifestStep{
			{Name: "my-extractor", Path: "steps/my-extractor/SKILL.md", License: "MIT", Version: "1.2.0"},
		},
	}
	srv := mockRegistryServer(t, manifest)
	defer srv.Close()

	// POST /api/v1/steps/install — simulates `ctxt pipeline step install`.
	body, err := json.Marshal(map[string]string{
		"name":          "my-extractor",
		"from_registry": srv.URL,
	})
	require.NoError(t, err)

	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusOK, resp.StatusCode, "install should return 200")

	var result map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "installed", result["status"])

	// Verify step appears in the local step list.
	listResp, err := gohttp.Get(env.URL + "/api/v1/steps")
	require.NoError(t, err)
	defer listResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, listResp.StatusCode)

	var listBody struct {
		Steps []storage.RegisteredStep `json:"steps"`
		Total int                      `json:"total"`
	}
	require.NoError(t, json.NewDecoder(listResp.Body).Decode(&listBody))

	var found *storage.RegisteredStep
	for i := range listBody.Steps {
		if listBody.Steps[i].Name == "my-extractor" {
			found = &listBody.Steps[i]
			break
		}
	}
	require.NotNil(t, found, "installed step must appear in /api/v1/steps list")
	assert.Contains(t, found.Source, "registry:")
}

func TestUS0108_InstallStep_GetAfterInstall(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	manifest := storage.RegistryManifest{
		Name:    "meta-registry",
		Version: "2.0",
		Steps: []storage.ManifestStep{
			{Name: "tokenizer-step", Path: "steps/tokenizer/SKILL.md", License: "Apache-2.0", Version: "3.1.0"},
		},
	}
	srv := mockRegistryServer(t, manifest)
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{
		"name":          "tokenizer-step",
		"from_registry": srv.URL,
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	// GET /api/v1/steps/{name} must return the step.
	getResp, err := gohttp.Get(env.URL + "/api/v1/steps/tokenizer-step")
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, getResp.StatusCode)

	var step storage.RegisteredStep
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&step))
	assert.Equal(t, "tokenizer-step", step.Name)
	assert.Contains(t, step.Source, "registry:")
}

func TestUS0108_InstallStep_MissingName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"from_registry": "http://example.com",
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0108_InstallStep_MissingRegistry(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	body, _ := json.Marshal(map[string]string{
		"name": "some-step",
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0108_InstallStep_StepNotInManifest(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	manifest := storage.RegistryManifest{
		Name:    "empty-registry",
		Version: "1.0",
		Steps:   []storage.ManifestStep{},
	}
	srv := mockRegistryServer(t, manifest)
	defer srv.Close()

	body, _ := json.Marshal(map[string]string{
		"name":          "nonexistent-step",
		"from_registry": srv.URL,
	})
	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, gohttp.StatusInternalServerError, resp.StatusCode)
}
