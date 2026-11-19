package config

import (
	"bytes"
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
// resolve to the user's entry. It is now a load error naming both keys.
func TestServerURLAcrossLayersIsError(t *testing.T) {
	_, err := layered(t,
		"server:\n  urls:\n    - {name: pers, url: 'http://pers.example.net:1'}\n",
		"server:\n  url: 'http://work.example.net:2'\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url")
	assert.Contains(t, err.Error(), "server.urls")
	assert.Contains(t, err.Error(), "http://work.example.net:2")
}

func TestServerURLAndURLsSameFileIsError(t *testing.T) {
	_, err := loadFromYAML(t, "server:\n  url: http://a.example.net\n  urls: [http://b.example.net]\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both set")
}

// server.url alone is a one-entry, unnamed server.urls.
func TestServerURLFoldsIntoURLs(t *testing.T) {
	cfg, err := loadFromYAML(t, "server:\n  url: http://a.example.net:7\n  token: tok\n")
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 1)
	assert.Equal(t, "http://a.example.net:7", cfg.Server.URLs[0].URL)
	assert.Empty(t, cfg.Server.URLs[0].Name)
	assert.Empty(t, cfg.Server.URL)
	assert.Equal(t, "http://a.example.net:7", cfg.Server.LegacyURL())
	assert.NotEmpty(t, cfg.Server.LegacyURLWarning())
}

func TestServerURLsAloneNoWarning(t *testing.T) {
	cfg, err := loadFromYAML(t, "server:\n  urls: [http://a.example.net]\n")
	require.NoError(t, err)
	assert.Empty(t, cfg.Server.LegacyURLWarning())
}

func TestServerURLInvalidStillFails(t *testing.T) {
	_, err := loadFromYAML(t, "server:\n  url: not-a-url\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url")
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

// A user-only server.url works as one entry and Bootstrap warns once.
func TestBootstrapWarnsLegacyURLOnce(t *testing.T) {
	var buf bytes.Buffer
	prevW, prevQ := DeprecationWriter, Quiet
	DeprecationWriter, Quiet = &buf, false
	t.Cleanup(func() { DeprecationWriter, Quiet = prevW, prevQ })

	home := hermeticHome(t)
	p := filepath.Join(home, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte("server:\n  url: http://a.example.net\n"), 0o644))
	cfg, _, err := Bootstrap("ctxt", []string{p}, nil, nil)
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 1)
	assert.Equal(t, 1, bytes.Count(buf.Bytes(), []byte("server.url is deprecated")), buf.String())

	buf.Reset()
	Quiet = true
	_, _, err = Bootstrap("ctxt", []string{p}, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestLintFlagsLegacyServerURL(t *testing.T) {
	cfg, err := loadFromYAML(t, "server:\n  url: http://a.example.net\n")
	require.NoError(t, err)
	var found bool
	for _, f := range LintConfig(cfg, "") {
		if f.Field == "server.url" && f.Severity == SeverityWarn {
			found = true
		}
	}
	assert.True(t, found)
}
