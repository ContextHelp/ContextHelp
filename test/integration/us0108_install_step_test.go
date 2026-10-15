package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stepArchive packs a minimal installable step named name.
func stepArchive(t *testing.T, name string) []byte {
	t.Helper()
	dir := t.TempDir()
	skill := "---\nname: " + name + "\nversion: 1.2.0\nlicense: MIT\nexecution:\n  type: external\n  executable: run.sh\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh\ncat\n"), 0o700)) // #nosec G306 -- test step executable
	b, err := steps.PackDir(dir)
	require.NoError(t, err)
	return b
}

// mockRegistryServer serves a registry manifest at its root listing each
// step's archive and that archive's sha256, and the archives under
// steps/<name>.tar.gz.
func mockRegistryServer(t *testing.T, names ...string) *httptest.Server {
	t.Helper()
	archives := map[string][]byte{}
	manifest := storage.RegistryManifest{Name: "test-registry", Version: "1.0", Steps: []storage.ManifestStep{}}
	for _, n := range names {
		a := stepArchive(t, n)
		sum := sha256.Sum256(a)
		archives["/steps/"+n+".tar.gz"] = a
		manifest.Steps = append(manifest.Steps, storage.ManifestStep{
			Name: n, Archive: "steps/" + n + ".tar.gz", SHA256: hex.EncodeToString(sum[:]), License: "MIT", Version: "1.2.0",
		})
	}
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)
	srv := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.URL.Path == "/" || r.URL.Path == "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(manifestJSON)
			return
		}
		if a, ok := archives[r.URL.Path]; ok {
			_, _ = w.Write(a)
			return
		}
		gohttp.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// withSteps gives env a steps directory and trusts the registry at url
// with trust (empty: not configured, so untrusted).
func withSteps(t *testing.T, env *testEnv, url string, trust config.RegistryTrustLevel) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "steps")
	env.svc.Installer.Dir = dir
	if trust != "" {
		env.svc.Cfg.Registries = append(env.svc.Cfg.Registries, config.RegistryConfig{Name: "test", URL: url, TrustLevel: trust})
	}
	return dir
}

func postInstall(t *testing.T, env *testEnv, req map[string]any) (*gohttp.Response, map[string]any) {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := gohttp.Post(env.URL+"/api/v1/steps/install", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestUS0108_InstallStep_DownloadAndRegister(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	srv := mockRegistryServer(t, "my-extractor")
	dir := withSteps(t, env, srv.URL, config.RegistryTrustLevelTrusted)

	resp, out := postInstall(t, env, map[string]any{"name": "my-extractor", "from_registry": srv.URL})
	require.Equal(t, gohttp.StatusOK, resp.StatusCode, "%v", out)
	assert.Equal(t, "installed", out["status"])
	assert.FileExists(t, filepath.Join(dir, "my-extractor", "run.sh"))

	getResp, err := gohttp.Get(env.URL + "/api/v1/steps/my-extractor")
	require.NoError(t, err)
	defer getResp.Body.Close()
	require.Equal(t, gohttp.StatusOK, getResp.StatusCode)
	var step storage.RegisteredStep
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&step))
	assert.Equal(t, "registry:"+srv.URL, step.Source)
	require.NotNil(t, step.Metadata)
	require.NotNil(t, step.Metadata.Execution)
	assert.Len(t, step.Metadata.Execution.ExecutableSHA256, 64)
	assert.Equal(t, "trusted", step.Metadata.Execution.TrustLevel)
}

func TestUS0108_InstallStep_UntrustedRegistryRefused(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	srv := mockRegistryServer(t, "my-extractor")
	dir := withSteps(t, env, srv.URL, "")

	resp, out := postInstall(t, env, map[string]any{"name": "my-extractor", "from_registry": srv.URL})
	assert.Equal(t, gohttp.StatusUnprocessableEntity, resp.StatusCode)
	errBody, _ := out["error"].(map[string]any)
	assert.Equal(t, "UNTRUSTED_REGISTRY", errBody["code"])
	assert.NoDirExists(t, filepath.Join(dir, "my-extractor"))
}

func TestUS0108_InstallStep_FromArchiveThenUninstall(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	dir := withSteps(t, env, "", "")

	resp, out := postInstall(t, env, map[string]any{"name": "local-step", "archive": stepArchive(t, "local-step")})
	require.Equal(t, gohttp.StatusOK, resp.StatusCode, "%v", out)
	require.DirExists(t, filepath.Join(dir, "local-step"))

	req, _ := gohttp.NewRequest(gohttp.MethodDelete, env.URL+"/api/v1/steps/local-step", nil)
	delResp, err := gohttp.DefaultClient.Do(req)
	require.NoError(t, err)
	delResp.Body.Close()
	assert.Equal(t, gohttp.StatusNoContent, delResp.StatusCode)
	assert.NoDirExists(t, filepath.Join(dir, "local-step"))
}

func TestUS0108_InstallStep_MissingName(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	resp, _ := postInstall(t, env, map[string]any{"from_registry": "http://example.com"})
	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0108_InstallStep_MissingSource(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	resp, _ := postInstall(t, env, map[string]any{"name": "some-step"})
	assert.Equal(t, gohttp.StatusBadRequest, resp.StatusCode)
}

func TestUS0108_InstallStep_StepNotInManifest(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)
	srv := mockRegistryServer(t)
	withSteps(t, env, srv.URL, config.RegistryTrustLevelTrusted)

	resp, _ := postInstall(t, env, map[string]any{"name": "nonexistent-step", "from_registry": srv.URL})
	assert.Equal(t, gohttp.StatusNotFound, resp.StatusCode)
}
