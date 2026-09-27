package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestServerURLsBareStrings: legacy form — every entry a plain URL string,
// no token.
func TestServerURLsBareStrings(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  urls:
    - https://primary.example.net:7700
    - http://127.0.0.1:8080
`)
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 2)
	assert.Equal(t, "https://primary.example.net:7700", cfg.Server.URLs[0].URL)
	assert.Empty(t, cfg.Server.URLs[0].Token)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Server.URLs[1].URL)
	assert.Empty(t, cfg.Server.URLs[1].Token)
}

// TestServerURLsObjectEntries: {url, token} mapping form carries a
// per-instance token.
func TestServerURLsObjectEntries(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  urls:
    - url: https://primary.example.net:7700
      token: tok-primary
    - url: http://127.0.0.1:8080
`)
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 2)
	assert.Equal(t, "https://primary.example.net:7700", cfg.Server.URLs[0].URL)
	assert.Equal(t, "tok-primary", cfg.Server.URLs[0].Token)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Server.URLs[1].URL)
	assert.Empty(t, cfg.Server.URLs[1].Token)
}

// TestServerURLsMixedEntries: bare strings and mappings mix freely.
func TestServerURLsMixedEntries(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  urls:
    - url: https://primary.example.net:7700
      token: tok-primary
    - http://127.0.0.1:8080
`)
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 2)
	assert.Equal(t, "tok-primary", cfg.Server.URLs[0].Token)
	assert.Equal(t, "http://127.0.0.1:8080", cfg.Server.URLs[1].URL)
}

// TestServerTokenDefault: a top-level server.token parses as the default
// credential for entries without their own.
func TestServerTokenDefault(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  token: tok-default
  urls:
    - http://127.0.0.1:8080
`)
	require.NoError(t, err)
	assert.Equal(t, "tok-default", cfg.Server.Token)
}

// TestServerURLSingle: the single-instance server.url form parses too.
func TestServerURLSingle(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  url: http://127.0.0.1:9999
`)
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9999", cfg.Server.URL)
}

// TestServerURLsMalformedEntryRejected: a bad entry must fail the load
// loudly, naming the entry — today it would silently probe as "down" and
// shift traffic elsewhere.
func TestServerURLsMalformedEntryRejected(t *testing.T) {
	cases := []struct{ name, yaml string }{
		{"no scheme", `server:
  urls:
    - primary.example.net:7700
`},
		{"bad scheme", `server:
  urls:
    - ftp://primary.example.net
`},
		{"empty entry", `server:
  urls:
    - ""
`},
		{"object without url", `server:
  urls:
    - token: tok-only
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadFromYAML(t, tc.yaml)
			require.Error(t, err, "malformed server.urls entry must fail the load")
			assert.Contains(t, err.Error(), "server.urls", "error must name the offending key")
		})
	}
}

// TestServerURLMalformedRejected: the single-URL form gets the same
// validation.
func TestServerURLMalformedRejected(t *testing.T) {
	_, err := loadFromYAML(t, `server:
  url: not a url at all
`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "server.url")
}

// TestServerURLsEntryBadShapeRejected: an entry that is neither a string
// nor a mapping is a parse error, not a silent zero value.
func TestServerURLsEntryBadShapeRejected(t *testing.T) {
	_, err := loadFromYAML(t, `server:
  urls:
    - [nested, list]
`)
	require.Error(t, err)
}

// TestServerURLsNamedEntries: a mapping entry takes an optional name; an
// entry without one, bare or mapping, stays unnamed.
func TestServerURLsNamedEntries(t *testing.T) {
	cfg, err := loadFromYAML(t, `server:
  urls:
    - name: home
      url: https://dpkms.example.net:7700
      token: tok-home
    - url: http://127.0.0.1:8080
    - http://127.0.0.1:8081
`)
	require.NoError(t, err)
	require.Len(t, cfg.Server.URLs, 3)
	assert.Equal(t, "home", cfg.Server.URLs[0].Name)
	assert.Equal(t, "tok-home", cfg.Server.URLs[0].Token)
	assert.Empty(t, cfg.Server.URLs[1].Name)
	assert.Empty(t, cfg.Server.URLs[2].Name)
}

// TestServerURLsBadNamesRejected: a name must be present-and-valid or
// absent. Duplicates, an explicit empty name, a non-slug and an all-digit
// name (which --instance would read as a port) fail the load, naming the
// entry.
func TestServerURLsBadNamesRejected(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"duplicate", `server:
  urls:
    - name: home
      url: https://a.example.net
    - name: home
      url: https://b.example.net
`, "duplicate"},
		{"empty when present", `server:
  urls:
    - name: ""
      url: https://a.example.net
`, "empty"},
		{"not a slug", `server:
  urls:
    - name: Home Box
      url: https://a.example.net
`, "name"},
		{"all digits", `server:
  urls:
    - name: "8080"
      url: https://a.example.net
`, "port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadFromYAML(t, tc.yaml)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "server.urls")
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestServerEndpointMarshalKeepsName: a named entry never collapses to
// the bare-string form, which would drop the name.
func TestServerEndpointMarshalKeepsName(t *testing.T) {
	out, err := yaml.Marshal([]ServerEndpoint{
		{Name: "home", URL: "https://a.example.net"},
		{URL: "http://127.0.0.1:8080"},
	})
	require.NoError(t, err)
	var back []ServerEndpoint
	require.NoError(t, yaml.Unmarshal(out, &back))
	require.Len(t, back, 2)
	assert.Equal(t, "home", back[0].Name)
	assert.Equal(t, "https://a.example.net", back[0].URL)
	assert.Contains(t, string(out), "- http://127.0.0.1:8080")
}
