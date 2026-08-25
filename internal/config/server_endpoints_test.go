package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
