package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
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

// lintConfigYAML returns a minimal valid config YAML.
func lintConfigYAML(extra string) []byte {
	return []byte("storage:\n  type: sqlite\n" + extra)
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
	content := "storage:\n  blob:\n    s3:\n      secret_key: \"sk-ant-verysecretvalue123456\"\n"
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

// TestConfigLoadNeverWritesFiles: no command writes a config file just by
// loading config. An unversioned user config, an unversioned -c file and
// -c key=value overlays stay byte-identical with unchanged mtimes across
// read-only commands, including validate, which re-loads the -c file.
func TestConfigLoadNeverWritesFiles(t *testing.T) {
	root := t.TempDir()
	xdg := filepath.Join(root, "config")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("CTXT_CONFIG", "")

	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	userCfg := filepath.Join(xdg, "contexthelp", "ctxt.yaml")
	write(userCfg, "server:\n  url: "+testguard.ClosedServerURL+"\n")
	cliCfg := filepath.Join(root, "cli", "ctxt.yaml")
	write(cliCfg, "server:\n  urls:\n    - http://127.0.0.1:19997\n")

	snapshot := func() map[string]string {
		t.Helper()
		out := map[string]string{}
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			body, err := os.ReadFile(p) // #nosec G304 -- test tempdir
			if err != nil {
				return err
			}
			out[p] = info.ModTime().String() + "\n" + string(body)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	overlay := []string{"-c", cliCfg, "-c", "storage.path=" + filepath.Join(t.TempDir(), "db.sqlite")}
	for _, args := range [][]string{
		{"config", "show"},
		{"config", "show", "--format", "json"},
		{"config", "validate", "--check-secrets=false"},
		{"config", "paths"},
		{"config", "doctor"},
		{"profile", "list"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			before := snapshot()
			_, _ = executeCommand(append(args, overlay...)...)
			after := snapshot()
			for p, a := range after {
				b, ok := before[p]
				switch {
				case !ok && filepath.Dir(p) == filepath.Dir(userCfg), !ok && filepath.Dir(p) == filepath.Dir(cliCfg):
					t.Errorf("%v created %s", args, p)
				case ok && a != b:
					t.Errorf("%v rewrote %s:\nbefore: %s\nafter: %s", args, p, b, a)
				}
			}
		})
	}
}
