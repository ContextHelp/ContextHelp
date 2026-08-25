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

// --- LintFinding.String ---

func TestLintFindingString(t *testing.T) {
	f := LintFinding{
		Severity: SeverityError,
		Field:    "storage.type",
		Message:  "unknown value",
	}
	s := f.String()
	assert.Contains(t, s, "ERROR")
	assert.Contains(t, s, "storage.type")
	assert.Contains(t, s, "unknown value")
}

func TestLintFindingStringWithFix(t *testing.T) {
	f := LintFinding{
		Severity: SeverityWarn,
		Field:    "@file",
		Message:  "world-readable",
		Fix:      "chmod 600 /tmp/config.yaml",
	}
	s := f.String()
	assert.Contains(t, s, "fix:")
	assert.Contains(t, s, "chmod 600")
}

// --- lintSchema ---

func TestLintSchemaErrors(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{Type: "badtype"},
		Server:  ServerConfig{Port: 80},
	}
	findings := lintSchema(cfg)
	require.NotEmpty(t, findings)
	for _, f := range findings {
		assert.Equal(t, SeverityError, f.Severity)
	}

	fields := make([]string, len(findings))
	for i, f := range findings {
		fields[i] = f.Field
	}
	assert.Contains(t, fields, "storage.type")
	assert.Contains(t, fields, "server.port")
}

func TestLintSchemaClean(t *testing.T) {
	cfg := &Config{
		Storage: StorageConfig{Type: "sqlite"},
		Jobs:    JobsConfig{PollInterval: 500 * time.Millisecond},
	}
	findings := lintSchema(cfg)
	assert.Empty(t, findings)
}

// --- lintSecrets ---

func TestLintSecretsFound(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Blob.S3.SecretKey = "sk-ant-verysecretvalue123456"
	findings := lintSecrets(cfg)
	require.NotEmpty(t, findings)
	assert.Equal(t, SeverityWarn, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "plaintext secret")
}

func TestLintSecretsEnvRef(t *testing.T) {
	cfg := &Config{}
	cfg.Storage.Blob.S3.SecretKey = "${MY_SECRET_KEY}"
	findings := lintSecrets(cfg)
	assert.Empty(t, findings)
}

// --- lintPermissions ---

func TestLintPermissionsWorldReadable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0644))

	findings := lintPermissions(cfgPath)
	require.NotEmpty(t, findings, "644 (world-readable) should produce a warning")
	assert.Equal(t, SeverityWarn, findings[0].Severity)
	assert.Equal(t, "@file", findings[0].Field)
	assert.Contains(t, findings[0].Message, "world-readable")
	assert.NotEmpty(t, findings[0].Fix, "should include a fix hint")
}

func TestLintPermissionsGroupReadable(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0640))

	findings := lintPermissions(cfgPath)
	require.NotEmpty(t, findings, "640 (group-readable) should produce a warning")
	assert.Equal(t, SeverityWarn, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "group-readable")
}

func TestLintPermissionsSafe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0600))

	findings := lintPermissions(cfgPath)
	assert.Empty(t, findings, "600 (owner-only) should be clean")
}

func TestLintPermissionsNoFile(t *testing.T) {
	findings := lintPermissions("/nonexistent/path/config.yaml")
	assert.Empty(t, findings, "missing file should not produce findings")
}

func TestLintPermissionsEmptyPath(t *testing.T) {
	findings := lintPermissions("")
	assert.Empty(t, findings)
}

// --- FixPermissions ---

func TestFixPermissions(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0644))

	require.NoError(t, FixPermissions(cfgPath))

	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "should be 600 after fix")
}

func TestFixPermissionsAlreadySafe(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0600))

	require.NoError(t, FixPermissions(cfgPath))

	info, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestFixPermissionsEmptyPath(t *testing.T) {
	assert.NoError(t, FixPermissions(""))
}

// --- LintConfig integration ---

func TestLintConfigCombined(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0644))

	cfg := &Config{}
	cfg.Storage.Type = "badtype"
	cfg.Storage.Blob.S3.SecretKey = "sk-ant-verysecretvalue123456"

	findings := LintConfig(cfg, cfgPath)

	severities := map[LintSeverity]int{}
	for _, f := range findings {
		severities[f.Severity]++
	}

	assert.GreaterOrEqual(t, severities[SeverityError], 1, "should have schema errors")
	assert.GreaterOrEqual(t, severities[SeverityWarn], 2, "should have secret + permission warnings")
}

func TestLintConfigClean(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0600))

	cfg := &Config{
		Storage: StorageConfig{
			Type: "sqlite",
			Path: "/home/user/.local/share/contexthelp/db.sqlite",
		},
	}

	findings := LintConfig(cfg, cfgPath)
	assert.Empty(t, findings, "clean config + safe permissions should have no findings")
}

func TestLintAccessPrivateWithFederationToken(t *testing.T) {
	cfg := &Config{}
	cfg.Federation.Token = "push-secret"

	findings := LintConfig(cfg, "")

	var hit *LintFinding
	for i, f := range findings {
		if f.Field == "federation.token" {
			hit = &findings[i]
			break
		}
	}
	require.NotNil(t, hit, "expected federation.token finding on private instance")
	assert.Equal(t, SeverityWarn, hit.Severity)
}

func TestLintPublicFlagDeprecationShorthand(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Public = true
	cfg.Server.Auth = AuthConfig{
		Provider: "static",
		Static:   StaticAuthConfig{Tokens: []StaticTokenConfig{{Token: "t", Principal: "p"}}},
	}

	findings := LintConfig(cfg, "")

	var hit *LintFinding
	for i, f := range findings {
		if f.Field == "server.public" {
			hit = &findings[i]
			break
		}
	}
	require.NotNil(t, hit, "expected server.public deprecation finding")
	assert.Equal(t, SeverityWarn, hit.Severity)
	assert.Contains(t, hit.Message, "deprecated")
}

func TestLintAccessProtectedWithFederationTokenIsQuiet(t *testing.T) {
	cfg := &Config{}
	cfg.Server.Access = AccessProtected
	cfg.Server.Auth = AuthConfig{
		Provider: "static",
		Static:   StaticAuthConfig{Tokens: []StaticTokenConfig{{Token: "t", Principal: "p"}}},
	}
	cfg.Federation.Token = "push-secret"

	for _, f := range LintConfig(cfg, "") {
		if f.Field == "federation.token" && f.Severity == SeverityWarn && f.Message != "" &&
			strings.Contains(f.Message, "private") {
			t.Fatalf("unexpected private-instance finding on protected instance: %v", f)
		}
	}
}
