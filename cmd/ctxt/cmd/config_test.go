package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigShow(t *testing.T) {
	out, err := executeCommand("config", "show")
	if err != nil {
		t.Fatalf("config show should succeed: %v", err)
	}
	if !strings.Contains(out, "Configuration:") {
		t.Error("output should contain 'Configuration:'")
	}
	if !strings.Contains(out, "storage:") {
		t.Error("output should contain storage section")
	}
	if !strings.Contains(out, "server:") {
		t.Error("output should contain server section")
	}
	if !strings.Contains(out, "profile:") {
		t.Error("output should contain profile section")
	}
}

func TestConfigPath(t *testing.T) {
	out, err := executeCommand("config", "path")
	if err != nil {
		t.Fatalf("config path should succeed: %v", err)
	}
	// After migration to kit/console/cli/config (T-0596): `path` prints
	// the highest-precedence *existing* file. When no real config is
	// found, kit emits the "<defaults>" sentinel — also valid output.
	if !strings.Contains(out, "config.yaml") &&
		!strings.Contains(out, "ctxt.yaml") &&
		!strings.Contains(out, "<defaults>") {
		t.Errorf("output should contain a config filename or <defaults> sentinel; got: %q", out)
	}
}

// TestConfigPaths covers the new (T-0597) `paths` subcommand. The full
// resolution chain is emitted one path per line in the default text
// format and always succeeds (a default sentinel rung means the chain
// is never empty).
func TestConfigPaths(t *testing.T) {
	out, err := executeCommand("config", "paths")
	if err != nil {
		t.Fatalf("config paths should succeed: %v", err)
	}
	if !strings.Contains(out, "<defaults>") &&
		!strings.Contains(out, "config.yaml") &&
		!strings.Contains(out, "ctxt.yaml") {
		t.Errorf("output should contain at least one rung of the resolution chain; got: %q", out)
	}
}

func TestConfigValidate(t *testing.T) {
	out, err := executeCommand("config", "validate")
	if err != nil {
		t.Fatalf("config validate should succeed: %v", err)
	}
	if !strings.Contains(out, "Configuration is valid") {
		t.Error("output should confirm valid configuration")
	}
}

// TestConfigValidateExitCode verifies that config validate exits 1 when
// plaintext secrets are present and 0 when the config is clean.
func TestConfigValidateExitCode(t *testing.T) {
	t.Run("exit 0 on clean config", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.yaml")
		if err := os.WriteFile(cfgPath, []byte("storage:\n  type: sqlite\n"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		out, err := executeCommand("--config", cfgPath, "config", "validate", "--check-secrets=true")
		if err != nil {
			t.Fatalf("expected exit 0 on clean config, got error: %v\noutput: %s", err, out)
		}
		if !strings.Contains(out, "No plaintext secrets detected") {
			t.Errorf("expected no-secrets message; got: %s", out)
		}
	})

	t.Run("exit 1 on plaintext secret", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.yaml")
		yamlContent := fmt.Sprintf("storage:\n  blob:\n    s3:\n      secret_key: %q\n", "sk-abcdefghijk12345678")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		out, err := executeCommand("--config", cfgPath, "config", "validate", "--check-secrets=true")
		if err == nil {
			t.Fatalf("expected exit 1 when plaintext secret present; output: %s", out)
		}
		if !strings.Contains(out, "plaintext secret") {
			t.Errorf("expected secret warning in output; got: %s", out)
		}
	})

	t.Run("exit 0 when check-secrets disabled", func(t *testing.T) {
		dir := t.TempDir()
		cfgPath := filepath.Join(dir, "config.yaml")
		yamlContent := fmt.Sprintf("storage:\n  blob:\n    s3:\n      secret_key: %q\n", "sk-abcdefghijk12345678")
		if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		out, err := executeCommand("--config", cfgPath, "config", "validate", "--check-secrets=false")
		if err != nil {
			t.Fatalf("expected exit 0 when check-secrets disabled; got: %v\noutput: %s", err, out)
		}
	})
}

func TestConfigHelp(t *testing.T) {
	out, err := executeCommand("config", "--help")
	if err != nil {
		t.Fatalf("config --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"show", "path", "validate", "edit", "doctor"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("config help should list subcommand %q", subcmd)
		}
	}
}

// lintConfigYAML returns a minimal valid config YAML with version: 1.
// Including version:1 prevents the Load() migration write-back from
// rewriting the file at 0600, which would mask intentional 0644 test setups.
func lintConfigYAML(extra string) []byte {
	return []byte("version: 1\nstorage:\n  type: sqlite\n" + extra)
}

// TestConfigLintClean verifies exit 0 on a clean, well-permissioned config.
func TestConfigLintClean(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, lintConfigYAML(""), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out, err := executeCommand("--config", cfgPath, "config", "doctor")
	if err != nil {
		t.Fatalf("expected exit 0 on clean config, got: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "no issues found") {
		t.Errorf("expected 'no issues found' in output; got: %s", out)
	}
}

// TestConfigLintPermissionWarn verifies a WARN is emitted for world-readable files.
func TestConfigLintPermissionWarn(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// version:1 prevents migration write-back from resetting permissions.
	if err := os.WriteFile(cfgPath, lintConfigYAML(""), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out, err := executeCommand("--config", cfgPath, "config", "doctor")
	if err == nil {
		t.Fatalf("expected exit 1 when config is world-readable; output: %s", out)
	}
	if !strings.Contains(out, "world-readable") {
		t.Errorf("expected 'world-readable' warning; got: %s", out)
	}
}

// TestConfigLintSecretWarn verifies a WARN is emitted for plaintext secrets.
func TestConfigLintSecretWarn(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// version:1 prevents migration write-back from interfering.
	content := "version: 1\nstorage:\n  blob:\n    s3:\n      secret_key: \"sk-ant-verysecretvalue123456\"\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out, err := executeCommand("--config", cfgPath, "config", "doctor")
	if err == nil {
		t.Fatalf("expected exit 1 when plaintext secret present; output: %s", out)
	}
	if !strings.Contains(out, "plaintext secret") {
		t.Errorf("expected secret warning; got: %s", out)
	}
}

// TestConfigLintFix verifies --fix auto-corrects world-readable permission.
func TestConfigLintFix(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// version:1 prevents migration write-back from masking the permission change.
	if err := os.WriteFile(cfgPath, lintConfigYAML(""), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Run with --fix; apply the permission fix.
	_, _ = executeCommand("--config", cfgPath, "config", "doctor", "--fix")

	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 after --fix, got %s", info.Mode().Perm())
	}
}

// TestConfigLintHelp verifies --fix flag is documented.
func TestConfigLintHelp(t *testing.T) {
	out, err := executeCommand("config", "doctor", "--help")
	if err != nil {
		t.Fatalf("config doctor --help should succeed: %v", err)
	}
	if !strings.Contains(out, "--fix") {
		t.Error("lint help should document --fix flag")
	}
}
