package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Config)
		wantField string
	}{
		{
			name:      "invalid storage type",
			mutate:    func(c *Config) { c.Storage.Type = "badtype" },
			wantField: "storage.type",
		},
		{
			name:      "port below range",
			mutate:    func(c *Config) { c.Server.Port = 80 },
			wantField: "server.port",
		},
		{
			name:      "grpc port above range",
			mutate:    func(c *Config) { c.Server.GRPCPort = 99999 },
			wantField: "server.grpc_port",
		},
		{
			name:      "cookie bridge port below range",
			mutate:    func(c *Config) { c.Server.CookieBridgePort = 443 },
			wantField: "server.cookie_bridge_port",
		},
		{
			name:      "poll interval too short",
			mutate:    func(c *Config) { c.Jobs.PollInterval = 10 * time.Millisecond },
			wantField: "jobs.poll_interval",
		},
		{
			name: "default profile not in profiles map",
			mutate: func(c *Config) {
				c.Profile.Default = "missing"
				c.Profile.Profiles = map[string]FocusProfile{"other": {}}
			},
			wantField: "profile.default",
		},
		{
			name: "invalid namespace format",
			mutate: func(c *Config) {
				c.Conventions.AllowedMentionNamespaces = []string{"BadNS"}
			},
			wantField: "conventions.allowed_mention_namespaces[0]",
		},
		{
			name: "quota missing principal",
			mutate: func(c *Config) {
				c.Server.Quotas = []ServerQuotaConfig{{Event: "entity_resolve", Limit: 10}}
			},
			wantField: "server.quotas[0].principal",
		},
		{
			name: "quota unknown event",
			mutate: func(c *Config) {
				c.Server.Quotas = []ServerQuotaConfig{{Principal: "partner", Event: "bad_event", Limit: 10}}
			},
			wantField: "server.quotas[0].event",
		},
		{
			name: "quota negative limit",
			mutate: func(c *Config) {
				c.Server.Quotas = []ServerQuotaConfig{{Principal: "partner", Event: "entity_resolve", Limit: -1}}
			},
			wantField: "server.quotas[0].limit",
		},
		{
			name: "quota warn threshold above limit",
			mutate: func(c *Config) {
				c.Server.Quotas = []ServerQuotaConfig{{Principal: "partner", Event: "content_pull", Limit: 10, WarnAt: 20}}
			},
			wantField: "server.quotas[0].warn_at",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)
			errs := Validate(cfg)
			assert.NotEmpty(t, errs)
			assert.Equal(t, tc.wantField, errs[0].Field)
		})
	}
}

// testStaticToken is a fake token long enough to pass CheckStaticToken.
const testStaticToken = "test-static-token-0123456789abcdef"

func staticAuth() AuthConfig {
	return AuthConfig{
		Provider: "static",
		Static: StaticAuthConfig{
			Tokens: []StaticTokenConfig{{Token: testStaticToken, Principal: "ops", Roles: []string{"admin"}}},
		},
	}
}

func TestValidateAccess(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(*Config)
		wantField string // "" = expect no errors
	}{
		{
			name:      "unknown access class",
			mutate:    func(c *Config) { c.Server.Access = "internal" },
			wantField: "server.access",
		},
		{
			name: "explicit private with public flag is a hard error",
			mutate: func(c *Config) {
				c.Server.Access = AccessPrivate
				c.Server.Public = true
			},
			wantField: "server.public",
		},
		{
			name:      "protected requires inbound auth",
			mutate:    func(c *Config) { c.Server.Access = AccessProtected },
			wantField: "server.access",
		},
		{
			name:      "public requires inbound auth",
			mutate:    func(c *Config) { c.Server.Access = AccessPublic },
			wantField: "server.access",
		},
		{
			name:      "public shorthand requires inbound auth",
			mutate:    func(c *Config) { c.Server.Public = true },
			wantField: "server.access",
		},
		{
			name: "static provider without tokens is not inbound auth",
			mutate: func(c *Config) {
				c.Server.Access = AccessProtected
				c.Server.Auth = AuthConfig{Provider: "static"}
			},
			wantField: "server.access",
		},
		{
			name: "protected with static auth is valid",
			mutate: func(c *Config) {
				c.Server.Access = AccessProtected
				c.Server.Auth = staticAuth()
			},
			wantField: "",
		},
		{
			name: "public with public flag and static auth is valid",
			mutate: func(c *Config) {
				c.Server.Access = AccessPublic
				c.Server.Public = true
				c.Server.Auth = staticAuth()
			},
			wantField: "",
		},
		{
			name:      "default private needs no auth",
			mutate:    func(_ *Config) {},
			wantField: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)
			errs := Validate(cfg)
			if tc.wantField == "" {
				assert.Empty(t, errs)
				return
			}
			assert.NotEmpty(t, errs)
			assert.Equal(t, tc.wantField, errs[0].Field)
		})
	}
}

func TestEffectiveAccess(t *testing.T) {
	cases := []struct {
		name   string
		server ServerConfig
		want   string
	}{
		{name: "unset defaults to private", server: ServerConfig{}, want: AccessPrivate},
		{name: "public flag is shorthand for public", server: ServerConfig{Public: true}, want: AccessPublic},
		{name: "explicit access wins over flag", server: ServerConfig{Access: AccessProtected, Public: true}, want: AccessProtected},
		{name: "explicit private", server: ServerConfig{Access: AccessPrivate}, want: AccessPrivate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.server.EffectiveAccess())
		})
	}
}

// Valid quota declarations produce no validation errors.
func TestValidateQuotasValid(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Quotas = []ServerQuotaConfig{
		{Principal: "partner", Event: "entity_resolve", Limit: 1000, WarnAt: 800},
		{Principal: "partner", Event: "content_pull"}, // no limit = unlimited
		{Principal: "other", Event: "taxonomy_sync", Limit: 5},
	}
	assert.Empty(t, Validate(cfg))
}

// Every static token needs at least one known role: roles are the only
// source of a token's scopes.
func TestValidateAuthRoles(t *testing.T) {
	cases := []struct {
		name    string
		roles   []string
		wantMsg string // "" = expect no errors
	}{
		{name: "known roles", roles: []string{"writer", "reader"}},
		{name: "no role", roles: nil, wantMsg: "at least one role"},
		{name: "unknown role", roles: []string{"admin", "owner"}, wantMsg: `unknown role "owner"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Auth = AuthConfig{
				Provider: "static",
				Static:   StaticAuthConfig{Tokens: []StaticTokenConfig{{Token: testStaticToken, Principal: "p", Roles: tc.roles}}},
			}
			errs := Validate(cfg)
			if tc.wantMsg == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Equal(t, "server.auth.static.tokens[0].roles", errs[0].Field)
			assert.Contains(t, errs[0].Message, tc.wantMsg)
		})
	}
}

// A static token guards a non-private instance: the example placeholder
// and short, guessable strings must never pass as one.
func TestCheckStaticToken(t *testing.T) {
	strong := "3f9c1a7be2d84605a1c7f0e9b8d2c4a63f9c1a7be2d84605a1c7f0e9b8d2c4a6"
	cases := []struct {
		name    string
		token   string
		wantMsg string // "" = accepted
	}{
		{name: "64 hex", token: strong},
		{name: "exactly minimum length", token: strings.Repeat("a", MinStaticTokenLen)},
		{name: "example placeholder", token: "REPLACE_ME_admin_token", wantMsg: "REPLACE_ME"},
		{name: "placeholder any case, long", token: "replace_me_" + strong, wantMsg: "REPLACE_ME"},
		{name: "placeholder mid-token", token: strong + "Replace_Me", wantMsg: "REPLACE_ME"},
		{name: "one short of minimum", token: strings.Repeat("a", MinStaticTokenLen-1), wantMsg: "at least 32"},
		{name: "short", token: "tok-1", wantMsg: "at least 32"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckStaticToken(tc.token)
			if tc.wantMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantMsg)
			assert.Contains(t, err.Error(), "openssl rand -hex 32", "error tells the operator how to generate one")
		})
	}
}

// Validate reports a weak static token against its entry.
func TestValidateAuthTokens(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Auth = staticAuth()
	cfg.Server.Auth.Static.Tokens = append(cfg.Server.Auth.Static.Tokens,
		StaticTokenConfig{Token: "REPLACE_ME_admin_token", Principal: "admin", Roles: []string{"admin"}})
	errs := Validate(cfg)
	require.Len(t, errs, 1)
	assert.Equal(t, "server.auth.static.tokens[1].token", errs[0].Field)
	assert.Contains(t, errs[0].Message, "REPLACE_ME")
}
