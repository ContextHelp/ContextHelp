package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layerFiles writes the given user and project config bodies (empty =
// no file) under a hermetic home, chdirs into the project and returns
// the user file path.
func layerFiles(t *testing.T, user, project string) {
	t.Helper()
	home := hermeticHome(t)
	proj := filepath.Join(home, "work", "proj")
	require.NoError(t, os.MkdirAll(proj, 0o755))
	if user != "" {
		dir := filepath.Join(home, ".config", "contexthelp")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ctxt.yaml"), []byte(user), 0o644))
	}
	if project != "" {
		dir := filepath.Join(proj, ".contexthelp")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ctxt.yaml"), []byte(project), 0o644))
	}
	withChdir(t, proj)
}

func TestEndpointUnit_ProjectURLBeatsUserURLs(t *testing.T) {
	layerFiles(t,
		"server:\n  urls:\n    - {name: pers, url: 'http://a.example.net:1', token: kA}\n",
		"server:\n  url: http://b.example.net:2\n  token: kB\n")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Empty(t, cfg.Server.URLs, "user server.urls must not survive a project endpoint")
	assert.Equal(t, "http://b.example.net:2", cfg.Server.URL)
	assert.Equal(t, "kB", cfg.Server.Token)
}

func TestEndpointUnit_ProjectURLsBeatsUserURL(t *testing.T) {
	layerFiles(t,
		"server:\n  url: http://a.example.net:1\n  token: kA\n",
		"server:\n  urls:\n    - {name: proj, url: 'http://b.example.net:2', token: kB}\n")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Empty(t, cfg.Server.URL)
	assert.Empty(t, cfg.Server.Token, "user token must not attach to a project endpoint")
	require.Len(t, cfg.Server.URLs, 1)
	assert.Equal(t, "proj", cfg.Server.URLs[0].Name)
}

func TestEndpointUnit_ProjectURLGetsNoUserToken(t *testing.T) {
	layerFiles(t,
		"server:\n  url: http://a.example.net:1\n  token: kA\n",
		"server:\n  url: http://b.example.net:2\n")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, "http://b.example.net:2", cfg.Server.URL)
	assert.Empty(t, cfg.Server.Token, "token absent in the owning layer means unauthenticated")
}

func TestEndpointUnit_UserOnlyUnchanged(t *testing.T) {
	layerFiles(t,
		"server:\n  token: kA\n  urls:\n    - {name: pers, url: 'http://a.example.net:1'}\n",
		"server:\n  port: 9876\n")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 1)
	assert.Equal(t, "kA", cfg.Server.Token)
	assert.Equal(t, 9876, cfg.Server.Port, "non-endpoint keys keep merging key by key")
}

func TestEndpointUnit_NoFileSetsAny(t *testing.T) {
	layerFiles(t, "", "server:\n  port: 9876\n")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Empty(t, cfg.Server.URL)
	assert.Empty(t, cfg.Server.URLs)
	assert.Empty(t, cfg.Server.Token)
}

func TestEndpointUnit_ExtraPathBeatsProject(t *testing.T) {
	layerFiles(t, "", "server:\n  url: http://b.example.net:2\n  token: kB\n")
	extra := filepath.Join(t.TempDir(), "extra.yaml")
	require.NoError(t, os.WriteFile(extra, []byte("server:\n  url: http://c.example.net:3\n"), 0o644))
	cfg, err := LoadWithOverrides("ctxt", "", []string{extra}, nil)
	require.NoError(t, err)
	assert.Equal(t, "http://c.example.net:3", cfg.Server.URL)
	assert.Empty(t, cfg.Server.Token)
}

func TestEndpointUnit_OverridesStillWin(t *testing.T) {
	layerFiles(t,
		"server:\n  urls:\n    - {name: pers, url: 'http://a.example.net:1'}\n",
		"server:\n  url: http://b.example.net:2\n  token: kB\n")
	cfg, err := LoadWithOverrides("ctxt", "", nil, map[string]any{
		"server": map[string]any{"token": "kO"},
	})
	require.NoError(t, err)
	assert.Equal(t, "http://b.example.net:2", cfg.Server.URL)
	assert.Equal(t, "kO", cfg.Server.Token, "overrides outrank the owning file")
	assert.Empty(t, cfg.Server.URLs)
}

func TestEndpointUnit_EnvStillWinsOverProject(t *testing.T) {
	layerFiles(t, "", "server:\n  url: http://b.example.net:2\n  token: kB\n  port: 9876\n")
	t.Setenv("CH_SERVER_PORT", "4242")
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, 4242, cfg.Server.Port, "env outranks the file that owns the endpoint unit")
	assert.Equal(t, "http://b.example.net:2", cfg.Server.URL)
}

func TestEndpointUnit_SystemBelowUser(t *testing.T) {
	// system slot is /etc/contexthelp: use the strict extra path shape
	// instead, where order is the same least-to-most-specific list.
	layerFiles(t, "server:\n  url: http://a.example.net:1\n  token: kA\n", "")
	system := filepath.Join(t.TempDir(), "system.yaml")
	require.NoError(t, os.WriteFile(system, []byte("server:\n  urls:\n    - http://s.example.net:9\n  token: kS\n"), 0o644))
	owner, err := endpointLayer([]string{system, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "contexthelp", "ctxt.yaml")})
	require.NoError(t, err)
	assert.Contains(t, owner, "contexthelp")
	owner, err = endpointLayer([]string{system})
	require.NoError(t, err)
	assert.Equal(t, system, owner)
}
