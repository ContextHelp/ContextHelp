package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registryLayerFixture writes a user-layer config and a project
// `.contexthelp/ctxt.yaml` and runs the test from inside the project.
func registryLayerFixture(t *testing.T, user, project string) (userPath, projectPath string) {
	t.Helper()
	home := hermeticHome(t)
	userPath = filepath.Join(home, ".config", "contexthelp", "ctxt.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(userPath), 0o755))
	require.NoError(t, os.WriteFile(userPath, []byte(user), 0o600))
	repo := filepath.Join(home, "src", "repo")
	projectPath = filepath.Join(repo, ".contexthelp", "ctxt.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectPath), 0o755))
	if project != "" {
		require.NoError(t, os.WriteFile(projectPath, []byte(project), 0o600))
	}
	withChdir(t, repo)
	// The walk-up starts at os.Getwd, which resolves symlinks (/tmp on
	// macOS).
	resolved, err := filepath.EvalSymlinks(projectPath)
	if err == nil {
		projectPath = resolved
	}
	return userPath, projectPath
}

const userRegistries = "registries:\n  - name: team\n    url: https://registry.example.com\n"

func TestRegistriesSource_ProjectLayerRedefinesList(t *testing.T) {
	_, projectPath := registryLayerFixture(t, userRegistries,
		"registries:\n  - name: team\n    url: https://attacker.example\n")

	src, found, err := RegistriesSource("ctxt", "", nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, LayerProject, src.Kind)
	assert.Equal(t, projectPath, src.Path)

	// The loader agrees: the project list replaced the user one.
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	require.Len(t, cfg.Registries, 1)
	assert.Equal(t, "https://attacker.example", cfg.Registries[0].URL)
}

func TestRegistriesSource_ProjectLayerWithoutRegistries(t *testing.T) {
	userPath, _ := registryLayerFixture(t, userRegistries, "server:\n  port: 9876\n")

	src, found, err := RegistriesSource("ctxt", "", nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, LayerUser, src.Kind)
	assert.Equal(t, userPath, src.Path)
}

func TestRegistriesSource_ExtraPathOutranksProject(t *testing.T) {
	_, _ = registryLayerFixture(t, userRegistries,
		"registries:\n  - name: team\n    url: https://attacker.example\n")
	extra := filepath.Join(t.TempDir(), "pinned.yaml")
	require.NoError(t, os.WriteFile(extra, []byte(userRegistries), 0o600))

	src, found, err := RegistriesSource("ctxt", "", []string{extra})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, LayerExtra, src.Kind)
	assert.Equal(t, extra, src.Path)
}

func TestRegistriesSource_NoFileSetsRegistries(t *testing.T) {
	_, _ = registryLayerFixture(t, "server:\n  port: 9876\n", "")

	_, found, err := RegistriesSource("ctxt", "", nil)
	require.NoError(t, err)
	assert.False(t, found)
}
