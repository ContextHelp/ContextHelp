package config

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/safefetch"
)

func TestFetchDefaultsBlockPrivate(t *testing.T) {
	cfg, err := loadFromYAML(t, "storage:\n  type: sqlite\n")
	require.NoError(t, err)
	assert.False(t, cfg.Fetch.AllowPrivate)
	assert.Empty(t, cfg.Fetch.AllowedHosts)
	assert.Empty(t, cfg.Fetch.AllowedCIDRs)

	err = FetchGuard(cfg).CheckURL(context.Background(), "http://169.254.169.254/latest/meta-data/")
	assert.True(t, errors.Is(err, safefetch.ErrBlocked), "default guard: %v", err)
	err = FetchGuard(nil).CheckURL(context.Background(), "http://10.0.0.1/")
	assert.True(t, errors.Is(err, safefetch.ErrBlocked), "nil config guard: %v", err)
}

func TestFetchAllowlistParse(t *testing.T) {
	cfg, err := loadFromYAML(t, `fetch:
  allow_private: false
  allowed_hosts:
    - Wiki.Corp.Example
  allowed_cidrs:
    - 10.20.0.0/16
    - 192.168.1.5
`)
	require.NoError(t, err)
	p, err := cfg.Fetch.Policy()
	require.NoError(t, err)
	assert.Equal(t, []string{"wiki.corp.example"}, p.AllowedHosts)
	assert.NoError(t, p.Check(netip.MustParseAddr("10.20.3.4")))
	assert.NoError(t, p.Check(netip.MustParseAddr("192.168.1.5")))
	assert.ErrorIs(t, p.Check(netip.MustParseAddr("192.168.1.6")), safefetch.ErrBlocked)

	g := FetchGuard(cfg)
	assert.NoError(t, g.CheckURL(context.Background(), "http://10.20.0.9/"))
	assert.NoError(t, g.CheckURL(context.Background(), "https://wiki.corp.example/"))

	cfg, err = loadFromYAML(t, "fetch:\n  allow_private: true\n")
	require.NoError(t, err)
	assert.NoError(t, FetchGuard(cfg).CheckURL(context.Background(), "http://127.0.0.1:9/"))
}

func TestFetchAllowlistMalformedRejected(t *testing.T) {
	for _, tc := range []struct{ yaml, field string }{
		{"fetch:\n  allowed_cidrs:\n    - 10.0.0.0/40\n", "fetch.allowed_cidrs[0]"},
		{"fetch:\n  allowed_cidrs:\n    - intranet\n", "fetch.allowed_cidrs[0]"},
		{"fetch:\n  allowed_hosts:\n    - ok.example\n    - http://wiki.example\n", "fetch.allowed_hosts[1]"},
		{"fetch:\n  allowed_hosts:\n    - \"*.corp.example\"\n", "fetch.allowed_hosts[0]"},
	} {
		_, err := loadFromYAML(t, tc.yaml)
		require.Error(t, err, tc.yaml)
		assert.Contains(t, err.Error(), tc.field)

		c := &Config{}
		c.Fetch.AllowedCIDRs = []string{"nope"}
		errs := Validate(c)
		var found bool
		for _, e := range errs {
			if e.Field == "fetch.allowed_cidrs[0]" {
				found = true
			}
		}
		assert.True(t, found, "Validate reports fetch.allowed_cidrs[0]: %v", errs)
	}
}
