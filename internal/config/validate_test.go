package config

import (
	"os"
	"path/filepath"
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
		{
			name:      "federation request limit below the pusher's request cap",
			mutate:    func(c *Config) { c.Federation.MaxRequestBytes = MinFederationMaxRequestBytes - 1 },
			wantField: "federation.max_request_bytes",
		},
		{
			name:      "federation request limit negative",
			mutate:    func(c *Config) { c.Federation.MaxRequestBytes = -1 },
			wantField: "federation.max_request_bytes",
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

// testKeyHash is a well-formed server.auth.keys[].hash.
const testKeyHash = "sha256:3f9c1a7be2d84605a1c7f0e9b8d2c4a63f9c1a7be2d84605a1c7f0e9b8d2c4a6"

func TestValidate_RegistryAuthTypesAccepted(t *testing.T) {
	for _, typ := range []RegistryAuthType{"", RegistryAuthBearer, RegistryAuthAPIKey, RegistryAuthOAuth2} {
		cfg := &Config{Registries: []RegistryConfig{{
			Name: "r", URL: "https://r.example",
			Auth: RegistryAuthConfig{Type: typ, HeaderName: "X-Registry-Key"},
		}}}
		assert.Empty(t, Validate(cfg), "auth.type %q", typ)
	}
}

func keysAuth() AuthConfig {
	return AuthConfig{Keys: []AuthKeyConfig{{Hash: testKeyHash, Principal: "owner", Roles: []string{"admin"}}}}
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
			name:      "protected mints links, so it needs public_url",
			mutate:    func(c *Config) { c.Server.Access = AccessProtected },
			wantField: "server.public_url",
		},
		{
			name:      "public needs public_url",
			mutate:    func(c *Config) { c.Server.Access = AccessPublic },
			wantField: "server.public_url",
		},
		{
			name:      "public shorthand needs public_url",
			mutate:    func(c *Config) { c.Server.Public = true },
			wantField: "server.public_url",
		},
		{
			name: "protected with public_url is valid: credentials live in the database",
			mutate: func(c *Config) {
				c.Server.Access = AccessProtected
				c.Server.PublicURL = "https://dpkms.example.net"
			},
			wantField: "",
		},
		{
			name: "protected minting no links needs no public_url",
			mutate: func(c *Config) {
				c.Server.Access = AccessProtected
				c.Server.Auth.SignIn = []string{}
			},
			wantField: "",
		},
		{
			name: "public with public flag, public_url and declared keys is valid",
			mutate: func(c *Config) {
				c.Server.Access = AccessPublic
				c.Server.Public = true
				c.Server.PublicURL = "https://dpkms.example.net"
				c.Server.Auth = keysAuth()
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

// A declared key's roles narrow it, so each must be a known bundle;
// none means the principal's own.
func TestValidateAuthKeyRoles(t *testing.T) {
	cases := []struct {
		name    string
		roles   []string
		wantMsg string // "" = expect no errors
	}{
		{name: "known roles", roles: []string{"writer", "reader"}},
		{name: "no role means the principal's", roles: nil},
		{name: "unknown role", roles: []string{"admin", "owner"}, wantMsg: `unknown role "owner"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Auth = AuthConfig{Keys: []AuthKeyConfig{{Hash: testKeyHash, Principal: "owner", Roles: tc.roles}}}
			errs := Validate(cfg)
			if tc.wantMsg == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1)
			assert.Equal(t, "server.auth.keys[0].roles", errs[0].Field)
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

// federation.max_request_bytes loads its default, takes the pusher's
// request cap as a floor, and 0 means the default.
func TestFederationMaxRequestBytes(t *testing.T) {
	cfg, err := Load("ctxt", "")
	require.NoError(t, err)
	assert.Equal(t, DefaultFederationMaxRequestBytes, cfg.Federation.MaxRequestBytes)

	for _, n := range []int64{0, MinFederationMaxRequestBytes} {
		c := &Config{Federation: FederationConfig{MaxRequestBytes: n}}
		assert.Empty(t, Validate(c), "max_request_bytes %d", n)
	}
	assert.Equal(t, DefaultFederationMaxRequestBytes, FederationConfig{}.RequestLimit())
	assert.Equal(t, int64(8<<20), FederationConfig{MaxRequestBytes: 8 << 20}.RequestLimit())
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

// server.auth.keys[] holds hashes only: a key in the clear, in any
// shape, is refused naming the entry; so are the example placeholder,
// a missing principal and an unparsable expiry.
func TestValidateAuthKeys(t *testing.T) {
	cases := []struct {
		name      string
		key       AuthKeyConfig
		wantField string // "" = accepted
		wantMsg   string
	}{
		{name: "hash", key: AuthKeyConfig{Hash: testKeyHash, Principal: "owner"}},
		{name: "hash with date expiry", key: AuthKeyConfig{Hash: testKeyHash, Principal: "owner", Expires: "2026-12-31"}},
		{name: "hash with rfc3339 expiry", key: AuthKeyConfig{Hash: testKeyHash, Principal: "prn_01J", Expires: "2026-12-31T10:00:00Z"}},
		{name: "the key itself", key: AuthKeyConfig{Hash: "ctxt_0123456789abcdef_" + strings.Repeat("A", 43), Principal: "owner"},
			wantField: "server.auth.keys[0].hash", wantMsg: "never the key itself"},
		{name: "a random secret", key: AuthKeyConfig{Hash: strings.Repeat("a", 64), Principal: "owner"},
			wantField: "server.auth.keys[0].hash", wantMsg: "sha256:<64 hex digits>"},
		{name: "wrong digest length", key: AuthKeyConfig{Hash: "sha256:abcd", Principal: "owner"},
			wantField: "server.auth.keys[0].hash", wantMsg: "sha256:<64 hex digits>"},
		{name: "upper-case hex", key: AuthKeyConfig{Hash: "sha256:" + strings.Repeat("A", 64), Principal: "owner"},
			wantField: "server.auth.keys[0].hash", wantMsg: "sha256:<64 hex digits>"},
		{name: "example placeholder", key: AuthKeyConfig{Hash: "sha256:REPLACE_ME", Principal: "owner"},
			wantField: "server.auth.keys[0].hash", wantMsg: "placeholder"},
		{name: "no principal", key: AuthKeyConfig{Hash: testKeyHash},
			wantField: "server.auth.keys[0].principal", wantMsg: "principal is required"},
		{name: "bad expiry", key: AuthKeyConfig{Hash: testKeyHash, Principal: "owner", Expires: "next year"},
			wantField: "server.auth.keys[0].expires", wantMsg: "RFC 3339"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Auth = AuthConfig{Keys: []AuthKeyConfig{tc.key}}
			errs := Validate(cfg)
			if tc.wantField == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1, "%v", errs)
			assert.Equal(t, tc.wantField, errs[0].Field)
			assert.Contains(t, errs[0].Message, tc.wantMsg)
		})
	}
	for _, k := range []AuthKeyConfig{{Expires: "2026-12-31"}, {Expires: "2026-12-31T10:00:00+02:00"}, {}} {
		at, err := k.ExpiresAt()
		require.NoError(t, err)
		switch k.Expires {
		case "":
			assert.True(t, at.IsZero())
		case "2026-12-31":
			assert.Equal(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), at, "a date expires at the end of that UTC day")
		default:
			assert.Equal(t, time.Date(2026, 12, 31, 8, 0, 0, 0, time.UTC), at)
		}
	}
}

// The method and sign-in lists hold known names once each; api_key is
// never optional; names ADR-079 reserves are refused by name until they
// ship.
func TestValidateAuthMethodsAndSignIn(t *testing.T) {
	cases := []struct {
		name    string
		auth    AuthConfig
		field   string // "" = accepted
		wantMsg string
	}{
		{name: "defaults", auth: AuthConfig{}},
		{name: "api_key only", auth: AuthConfig{Methods: []string{"api_key"}, SignIn: []string{}}},
		{name: "any order", auth: AuthConfig{Methods: []string{"session", "api_key"}}},
		{name: "unknown method", auth: AuthConfig{Methods: []string{"api_key", "cookie"}}, field: "server.auth.methods", wantMsg: `unknown entry "cookie"`},
		{name: "reserved method", auth: AuthConfig{Methods: []string{"api_key", "proxy"}}, field: "server.auth.methods", wantMsg: `"proxy" is reserved`},
		{name: "repeated method", auth: AuthConfig{Methods: []string{"api_key", "session", "session"}}, field: "server.auth.methods", wantMsg: "listed twice"},
		{name: "without api_key", auth: AuthConfig{Methods: []string{"session"}}, field: "server.auth.methods", wantMsg: "api_key is required"},
		{name: "unknown sign-in", auth: AuthConfig{SignIn: []string{"password"}}, field: "server.auth.sign_in", wantMsg: `unknown entry "password"`},
		{name: "reserved sign-in", auth: AuthConfig{SignIn: []string{"passkey"}}, field: "server.auth.sign_in", wantMsg: `"passkey" is reserved`},
		{name: "repeated sign-in", auth: AuthConfig{SignIn: []string{"invite", "invite"}}, field: "server.auth.sign_in", wantMsg: "listed twice"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Auth = tc.auth
			errs := Validate(cfg)
			if tc.field == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1, "%v", errs)
			assert.Equal(t, tc.field, errs[0].Field)
			assert.Contains(t, errs[0].Message, tc.wantMsg)
		})
	}
	assert.Equal(t, DefaultAuthMethods, AuthConfig{}.EffectiveMethods())
	assert.Equal(t, DefaultAuthSignIn, AuthConfig{}.EffectiveSignIn())
	assert.Empty(t, AuthConfig{SignIn: []string{}}.EffectiveSignIn(), "an explicit empty list disables every ceremony")
	assert.True(t, AuthConfig{}.NeedsPublicURL())
	assert.False(t, AuthConfig{SignIn: []string{}}.NeedsPublicURL())
}

// server.public_url is the base of every link: a non-private instance
// whose sign-in list builds links must name an absolute http(s) URL
// with nothing after the path. A private instance and one minting no
// links need none.
func TestValidatePublicURL(t *testing.T) {
	for raw, wantMsg := range map[string]string{
		"https://dpkms.example.net":      "",
		"http://127.0.0.1:8080":          "",
		"https://dpkms.example.net/base": "",
		"":                               "required",
		"dpkms.example.net":              "absolute http or https",
		"ftp://dpkms.example.net":        "absolute http or https",
		"https://":                       "absolute http or https",
		"https://dpkms.example.net/?x=1": "no query",
		"https://u:p@dpkms.example.net":  "no query, fragment or credentials",
	} {
		t.Run(raw, func(t *testing.T) {
			cfg := &Config{}
			cfg.Server.Access = AccessProtected
			cfg.Server.PublicURL = raw
			errs := Validate(cfg)
			if wantMsg == "" {
				assert.Empty(t, errs)
				return
			}
			require.Len(t, errs, 1, "%v", errs)
			assert.Equal(t, "server.public_url", errs[0].Field)
			assert.Contains(t, errs[0].Message, wantMsg)
		})
	}
	private := &Config{}
	assert.Empty(t, Validate(private), "a private instance needs no public_url")
	assert.NoError(t, private.ValidateAccess())
	broken := &Config{}
	broken.Server.Access = AccessProtected
	broken.Server.Auth.Keys = []AuthKeyConfig{{Hash: "ctxt_plain", Principal: "owner"}}
	err := broken.ValidateAccess()
	require.Error(t, err, "serve refuses a broken server.auth before binding a port")
	assert.Contains(t, err.Error(), "server.auth.keys[0].hash")
	assert.Contains(t, err.Error(), "server.public_url")
}

// A YAML file distinguishes an absent sign_in (the default list) from
// an explicit empty one (no ceremony at all), through the loader.
func TestLoadAuthSignInUnsetVersusEmpty(t *testing.T) {
	clearPortEnv(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvConfigPath, "")
	for body, want := range map[string][]string{
		"server:\n  access: protected\n  public_url: https://x.example\n": DefaultAuthSignIn,
		"server:\n  access: protected\n  auth:\n    sign_in: []\n":        {},
		"server:\n  access: protected\n  public_url: https://x.example\n  auth:\n    sign_in: [link]\n    methods: [api_key]\n    keys:\n      - hash: " + testKeyHash + "\n        principal: owner\n        expires: 2026-12-31\n": {"link"},
	} {
		path := filepath.Join(t.TempDir(), "dpkms.yaml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		cfg, err := Load("dpkms", path)
		require.NoError(t, err, body)
		assert.Equal(t, want, cfg.Server.Auth.EffectiveSignIn(), body)
		assert.Empty(t, Validate(cfg), body)
	}
}

// Load does not expand ${VAR}: such a value is used literally (a token
// of "${TOKEN}"), so Validate rejects it wherever it appears.
func TestValidateRejectsUnexpandedEnvRefs(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Blob.S3.SecretKey = "${S3_SECRET_KEY}"
	cfg.Storage.Path = "postgres://u:${PGPASSWORD}@db/ctxt"
	cfg.Storage.Blob.S3.AccessKey = "$S3_ACCESS_KEY"
	cfg.Server.Token = "${DEVICE_TOKEN}"

	got := map[string]string{}
	for _, e := range Validate(cfg) {
		got[e.Field] = e.Message
	}
	for _, field := range []string{
		"storage.blob.s3.secret_key",
		"storage.path",
		"storage.blob.s3.access_key",
		"server.token",
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
