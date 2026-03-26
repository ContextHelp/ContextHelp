package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanSecretsFindsPlaintext(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Config)
		wantField  string
		wantInHint string
	}{
		{
			name: "openai sk- key in S3 access_key",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.AccessKey = "sk-supersecretapikey1234"
			},
			wantField:  "storage.blob.s3.access_key",
			wantInHint: "sk-s",
		},
		{
			name: "anthropic sk-ant- key",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.SecretKey = "sk-ant-api123456789abcdef"
			},
			wantField:  "storage.blob.s3.secret_key",
			wantInHint: "sk-a",
		},
		{
			name: "JWT token (eyJ prefix)",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.AccessKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"
			},
			wantField:  "storage.blob.s3.access_key",
			wantInHint: "eyJh",
		},
		{
			name: "GitHub PAT ghp_ prefix",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.AccessKey = "ghp_16C7e42F292c6912E7710c838347Ae178B4a"
			},
			wantField:  "storage.blob.s3.access_key",
			wantInHint: "ghp_",
		},
		{
			name: "field named secret_key with long value",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.SecretKey = "myVeryLongPlaintextSecretValueHere"
			},
			wantField:  "storage.blob.s3.secret_key",
			wantInHint: "myVe",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)
			warnings := ScanSecrets(cfg)
			require.NotEmpty(t, warnings, "expected at least one warning")

			found := false
			for _, w := range warnings {
				if w.Field == tc.wantField {
					found = true
					assert.Contains(t, w.Hint, tc.wantInHint)
					assert.NotEmpty(t, w.Reason)
				}
			}
			assert.True(t, found, "expected warning for field %q; got %v", tc.wantField, warnings)
		})
	}
}

func TestScanSecretsSkipsEnvRef(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{
			name: "braces env ref ${VAR}",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.SecretKey = "${MY_SECRET_KEY}"
			},
		},
		{
			name: "bare env ref $VAR",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.SecretKey = "$MY_SECRET_KEY"
			},
		},
		{
			name: "openai key as env ref",
			mutate: func(c *Config) {
				c.Storage.Blob.S3.AccessKey = "${OPENAI_API_KEY}"
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{}
			tc.mutate(cfg)
			warnings := ScanSecrets(cfg)
			assert.Empty(t, warnings, "env-var references must not produce warnings")
		})
	}
}

func TestScanSecretsCleanConfig(t *testing.T) {
	// A default-like config with no secrets should produce no warnings.
	cfg := &Config{
		Storage: StorageConfig{
			Type: "sqlite",
			Path: "/home/user/.local/share/contexthelp/db.sqlite",
			Blob: BlobConfig{
				Backend: "local",
				Local:   BlobLocalConfig{Path: "/home/user/.local/share/contexthelp/blobs"},
				S3: BlobS3Config{
					Region: "us-east-1",
				},
			},
		},
		Secrets: SecretsConfig{
			Backend:         "env",
			KeychainService: "ctxt",
		},
	}
	warnings := ScanSecrets(cfg)
	assert.Empty(t, warnings, "clean config should produce no warnings; got %v", warnings)
}

func TestSanitise(t *testing.T) {
	assert.Equal(t, "sk-s****", sanitise("sk-supersecret"))
	assert.Equal(t, "****", sanitise("abc"))
	assert.Equal(t, "abcd****", sanitise("abcdefgh"))
}
