package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layerFile writes body to dir/name and returns the path.
func layerFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	writeYAML(t, p, body)
	return p
}

// Load ignores keys Config does not declare; UnknownKeys names each one
// with its line so `config validate` can warn about it.
func TestUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	p := layerFile(t, dir, "ctxt.yaml", "storage:\n  type: sqlite\n  bogus_key: 1\nnot_a_section: true\n")

	got, err := UnknownKeys(p)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Contains(t, got[0], "line 3")
	assert.Contains(t, got[0], "bogus_key")
	assert.Contains(t, got[1], "not_a_section")
}

func TestUnknownKeysClean(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"known.yaml": "storage:\n  type: sqlite\nserver:\n  port: 8080\n",
		"empty.yaml": "",
	} {
		got, err := UnknownKeys(layerFile(t, dir, name, body))
		require.NoError(t, err, name)
		assert.Empty(t, got, name)
	}
}

// LayerFiles lists the files LoadWithOverrides merges, lowest precedence
// first: CTXT_CONFIG short-circuits the cascade, -c paths follow, and a
// slot whose file does not exist is left out.
func TestLayerFiles(t *testing.T) {
	dir := t.TempDir()
	envFile := layerFile(t, dir, "env.yaml", "")
	extra := layerFile(t, dir, "extra.yaml", "")

	t.Setenv(EnvConfigPath, envFile)
	assert.Equal(t, []string{envFile, extra}, LayerFiles("ctxt", "", []string{extra}))

	t.Setenv(EnvConfigPath, filepath.Join(dir, "missing.yaml"))
	assert.Equal(t, []string{extra}, LayerFiles("ctxt", "", []string{extra}))

	// A legacy cfgFile replaces the cascade and leads the extra paths.
	assert.Equal(t, []string{envFile, extra}, LayerFiles("ctxt", envFile, []string{extra}))
}
