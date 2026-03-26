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
	if !strings.Contains(out, "config.yaml") {
		t.Error("output should contain config filename")
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
	for _, subcmd := range []string{"show", "path", "validate", "edit"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("config help should list subcommand %q", subcmd)
		}
	}
}
