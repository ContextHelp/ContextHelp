package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func staticAuth() AuthConfig {
	return AuthConfig{
		Provider: "static",
		Static: StaticAuthConfig{
			Tokens: []StaticTokenConfig{{Token: "tok-1", Principal: "ops"}},
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
