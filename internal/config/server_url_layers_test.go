package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// layered writes a user layer and a project layer (.contexthelp/ctxt.yaml)
// and loads from the project dir. An empty body writes no file.
func layered(t *testing.T, user, project string) (*Config, error) {
	t.Helper()
	home := hermeticHome(t)
	if user != "" {
		dir := filepath.Join(home, ".config", "contexthelp")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ctxt.yaml"), []byte(user), 0o644))
	}
	proj := filepath.Join(home, "work", "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".contexthelp"), 0o755))
	if project != "" {
		require.NoError(t, os.WriteFile(filepath.Join(proj, ".contexthelp", "ctxt.yaml"), []byte(project), 0o644))
	}
	withChdir(t, proj)
	return Load("ctxt", "")
}

// kit replaces slices whole: the closest layer that sets server.urls wins.
func TestServerURLsProjectBeatsUser(t *testing.T) {
	cfg, err := layered(t,
		"server:\n  urls:\n    - {name: pers, url: 'http://pers.example.net:1'}\n",
		"server:\n  urls:\n    - {name: work, url: 'http://work.example.net:2'}\n")
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 1)
	assert.Equal(t, "work", cfg.Server.URLs[0].Name)
}

// The original misroute: user server.urls plus project server.url used to
// resolve to the user's entry. server.url is now a load error.
func TestServerURLAcrossLayersIsError(t *testing.T) {
	_, err := layered(t,
		"server:\n  urls:\n    - {name: pers, url: 'http://pers.example.net:1'}\n",
		"server:\n  url: 'http://work.example.net:2'\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url is no longer supported")
	assert.Contains(t, err.Error(), "http://work.example.net:2")
}

func TestServerURLAndURLsSameFileIsError(t *testing.T) {
	_, err := loadFromYAML(t, "server:\n  url: http://a.example.net\n  urls: [http://b.example.net]\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url is no longer supported")
}

// server.url alone is a load error: kit ignores unknown keys, so a silent
// drop would route to the default endpoint.
func TestServerURLAloneIsError(t *testing.T) {
	_, err := loadFromYAML(t, "server:\n  url: http://a.example.net:7\n  token: tok\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url is no longer supported")
	assert.Contains(t, err.Error(), "urls: [http://a.example.net:7]")
}

func TestServerURLInAnyLayerIsError(t *testing.T) {
	_, err := layered(t, "server:\n  url: http://a.example.net\n", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url is no longer supported")
}

func TestServerURLsAloneLoads(t *testing.T) {
	cfg, err := loadFromYAML(t, "server:\n  urls: [http://a.example.net]\n")
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 1)
}

func TestValidateFlagsServerURL(t *testing.T) {
	errs := Validate(&Config{Server: ServerConfig{URL: "http://a.example.net"}})
	var found bool
	for _, e := range errs {
		found = found || e.Field == "server.url"
	}
	assert.True(t, found)
	var lint bool
	for _, f := range LintConfig(&Config{Server: ServerConfig{URL: "http://a.example.net"}}, "") {
		lint = lint || (f.Field == "server.url" && f.Severity == SeverityError)
	}
	assert.True(t, lint)
}

// server.token layers like any scalar: a user default token applies to a
// project's server.urls entries.
func TestServerTokenLayersAcrossURLs(t *testing.T) {
	cfg, err := layered(t,
		"server:\n  token: tok-user\n",
		"server:\n  urls: [http://work.example.net:2]\n")
	require.NoError(t, err)
	assert.Equal(t, "tok-user", cfg.Server.Token)
	require.Len(t, cfg.Server.URLs, 1)
	assert.Empty(t, cfg.Server.URLs[0].Token)
}
