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
		{
			name: "registry unknown auth type",
			mutate: func(c *Config) {
				c.Registries = []RegistryConfig{
					{Name: "ok", URL: "https://a.example", Auth: RegistryAuthConfig{Type: RegistryAuthBearer}},
					{Name: "bad", URL: "https://b.example", Auth: RegistryAuthConfig{Type: "basic"}},
				}
			},
			wantField: "registries[1].auth.type",
		},
		{
			name: "registry invalid header name",
			mutate: func(c *Config) {
				c.Registries = []RegistryConfig{{
					Name: "bad", URL: "https://b.example",
					Auth: RegistryAuthConfig{Type: RegistryAuthAPIKey, HeaderName: "X API Key"},
				}}
			},
			wantField: "registries[0].auth.header_name",
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

func TestValidate_RegistryAuthTypesAccepted(t *testing.T) {
	for _, typ := range []RegistryAuthType{"", RegistryAuthBearer, RegistryAuthAPIKey, RegistryAuthOAuth2} {
		cfg := &Config{Registries: []RegistryConfig{{
			Name: "r", URL: "https://r.example",
			Auth: RegistryAuthConfig{Type: typ, HeaderName: "X-Registry-Key"},
		}}}
		assert.Empty(t, Validate(cfg), "auth.type %q", typ)
	}
}

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

// fieldsOf returns the Field of every error, in order.
func fieldsOf(errs []ValidationError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Field
	}
	return out
}

// Validate is the one config validator: it reports every problem, from
// every section, in one pass rather than stopping at the first.
func TestValidateReportsEveryError(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Port = 80
	cfg.Duplicates.Policy = "delete"
	cfg.Storage.Blob.Backend = "stub"
	cfg.Embeddings.MinCoverage = 2
	cfg.Federations = []FederationEntry{
		{Name: "a", SyncMode: "push"},
		{Name: "b", SyncMode: "async"},
	}

	fields := fieldsOf(Validate(cfg))
	for _, want := range []string{
		"server.port",
		"duplicates.policy",
		"storage.blob.backend",
		"embeddings.min_coverage",
		"federations[0].sync_mode",
		"federations[1].interval",
	} {
		assert.Contains(t, fields, want)
	}
}

// A loaded default config is valid: `config validate` on a fresh
// install exits 0.
func TestValidateDefaultsClean(t *testing.T) {
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Empty(t, Validate(cfg))
}

func TestValidateBlob(t *testing.T) {
	cases := []struct {
		name      string
		blob      BlobConfig
		wantField string // "" = expect no errors
	}{
		{name: "default backend", blob: BlobConfig{}},
		{name: "local", blob: BlobConfig{Backend: "local", Local: BlobLocalConfig{Path: "/data/blobs"}}},
		{name: "s3 with bucket", blob: BlobConfig{Backend: "s3", S3: BlobS3Config{Bucket: "b"}}},
		{name: "garage with endpoint and bucket", blob: BlobConfig{Backend: "garage", S3: BlobS3Config{Bucket: "b", Endpoint: "http://localhost:3900"}}},
		{name: "unknown backend", blob: BlobConfig{Backend: "stub"}, wantField: "storage.blob.backend"},
		{name: "s3 without bucket", blob: BlobConfig{Backend: "s3"}, wantField: "storage.blob.s3.bucket"},
		{name: "garage without endpoint", blob: BlobConfig{Backend: "garage", S3: BlobS3Config{Bucket: "b"}}, wantField: "storage.blob.s3.endpoint"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Storage.Blob = tc.blob
			errs := Validate(cfg)
			if tc.wantField == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Equal(t, tc.wantField, errs[0].Field)
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

// Load does not expand ${VAR}: such a value is used literally (a token
// of "${TOKEN}"), so Validate rejects it wherever it appears.
func TestValidateRejectsUnexpandedEnvRefs(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Blob.S3.SecretKey = "${S3_SECRET_KEY}"
	cfg.Storage.Path = "postgres://u:${PGPASSWORD}@db/ctxt"
	cfg.Storage.Blob.S3.AccessKey = "$S3_ACCESS_KEY"
	cfg.Server.Auth = AuthConfig{
		Provider: "static",
		Static:   StaticAuthConfig{Tokens: []StaticTokenConfig{{Token: "${DEVICE_TOKEN}", Principal: "p", Roles: []string{"reader"}}}},
	}

	got := map[string]string{}
	for _, e := range Validate(cfg) {
		got[e.Field] = e.Message
	}
	for _, field := range []string{
		"storage.blob.s3.secret_key",
		"storage.path",
		"storage.blob.s3.access_key",
		"server.auth.static.tokens[0].token",
	} {
		assert.Equal(t, envRefMessage, got[field], field)
	}
	assert.Equal(t, "config does not expand ${VAR}; set the value literally, use the bound env var, or a second --config secrets file", envRefMessage)
}

// Only shell-style references are rejected: a literal dollar sign, a
// GitHub Actions expression and a regex anchor are ordinary values.
func TestValidateAllowsLiteralDollars(t *testing.T) {
	for _, v := range []string{"pa$$word-1234", "${{ secrets.TOKEN }}", "^https://example\\.com/$", "$2a$10$abcdefghijkl"} {
		cfg := &Config{}
		cfg.Storage.Blob.S3.SecretKey = v
		assert.Empty(t, Validate(cfg), v)
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
