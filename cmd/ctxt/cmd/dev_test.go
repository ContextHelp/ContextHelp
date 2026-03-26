package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── validate-registry ───────────────────────────────────────────────────────

func TestDevValidateRegistry_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `name: "Test Registry"
version: "1.0.0"
taxonomy:
  - namespace: tech
    title: Technology
  - namespace: lang
    title: Language
steps:
  - name: my-step
    path: /steps/my-step
    version: "0.1.0"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("dev", "validate-registry", path)
	if err != nil {
		t.Fatalf("expected success, got error: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "OK") && !strings.Contains(out, "valid") {
		t.Errorf("expected OK/valid in output, got: %s", out)
	}
}

func TestDevValidateRegistry_MissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `version: "1.0.0"
taxonomy:
  - namespace: tech
    title: Technology
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand("dev", "validate-registry", path)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should mention 'validation failed', got: %v", err)
	}
}

func TestDevValidateRegistry_InvalidSlug(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `name: "Test Registry"
version: "1.0.0"
taxonomy:
  - namespace: "Bad Slug!"
    title: Bad
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand("dev", "validate-registry", path)
	if err == nil {
		t.Fatal("expected error for invalid slug, got nil")
	}
}

func TestDevValidateRegistry_DuplicateNamespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `name: "Test Registry"
version: "1.0.0"
taxonomy:
  - namespace: tech
    title: Technology
  - namespace: tech
    title: Technology Dup
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("dev", "validate-registry", path)
	if err == nil {
		t.Fatalf("expected error for duplicate namespace, out: %s", out)
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should mention 'validation failed', got: %v", err)
	}
}

func TestDevValidateRegistry_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `name: "Test Registry"
version: "1.0.0"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("--output", "json", "dev", "validate-registry", path)
	if err != nil {
		t.Fatalf("expected success, got: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output not valid JSON: %v\nout: %s", err, out)
	}
	if v, ok := result["valid"].(bool); !ok || !v {
		t.Errorf("expected valid=true in JSON output, got: %v", result)
	}
}

func TestDevValidateRegistry_DuplicateStep(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "registry.yaml")
	content := `name: "Test Registry"
version: "1.0.0"
steps:
  - name: my-step
    path: /a
  - name: my-step
    path: /b
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := executeCommand("dev", "validate-registry", path)
	if err == nil {
		t.Fatal("expected error for duplicate step name, got nil")
	}
}

// ─── init-plugin ─────────────────────────────────────────────────────────────

func TestDevInitPlugin_Basic(t *testing.T) {
	dir := t.TempDir()
	out, err := executeCommand("dev", "init-plugin", "my-plugin", "--dir", dir)
	if err != nil {
		t.Fatalf("expected success, got error: %v\nout: %s", err, out)
	}

	pluginDir := filepath.Join(dir, "my-plugin")
	for _, name := range []string{"manifest.yaml", "plugin.go", "README.md"} {
		path := filepath.Join(pluginDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file %s to exist: %v", name, err)
		}
	}
}

func TestDevInitPlugin_ManifestContent(t *testing.T) {
	dir := t.TempDir()
	_, err := executeCommand("dev", "init-plugin", "cool-thing", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "cool-thing", "manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "cool-thing") {
		t.Errorf("manifest.yaml should contain slug, got: %s", string(data))
	}
}

func TestDevInitPlugin_GoStubContent(t *testing.T) {
	dir := t.TempDir()
	_, err := executeCommand("dev", "init-plugin", "my-plugin", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "my-plugin", "plugin.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{"pluginapi.Plugin", "Name()", "Version()", "Init(", "Close("} {
		if !strings.Contains(src, want) {
			t.Errorf("plugin.go should contain %q", want)
		}
	}
}

func TestDevInitPlugin_InvalidSlug(t *testing.T) {
	dir := t.TempDir()
	_, err := executeCommand("dev", "init-plugin", "Bad Slug!", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for invalid slug, got nil")
	}
}

func TestDevInitPlugin_ExistingDir(t *testing.T) {
	dir := t.TempDir()
	// Create the plugin dir first.
	if err := os.MkdirAll(filepath.Join(dir, "my-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := executeCommand("dev", "init-plugin", "my-plugin", "--dir", dir)
	if err == nil {
		t.Fatal("expected error for existing dir, got nil")
	}
}

func TestDevInitPlugin_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	out, err := executeCommand("--output", "json", "dev", "init-plugin", "json-plugin", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output not valid JSON: %v\nout: %s", err, out)
	}
	if result["slug"] != "json-plugin" {
		t.Errorf("expected slug=json-plugin, got: %v", result["slug"])
	}
}

// ─── gen-docs ────────────────────────────────────────────────────────────────

func TestDevGenDocs_Basic(t *testing.T) {
	dir := t.TempDir()
	out, err := executeCommand("dev", "gen-docs", "--dir", dir)
	if err != nil {
		t.Fatalf("expected success, got: %v\nout: %s", err, out)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected markdown files to be written")
	}

	// ctxt.md (root) and at least a few subcommand files.
	found := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one .md file in output dir")
	}
}

func TestDevGenDocs_ContainsDev(t *testing.T) {
	dir := t.TempDir()
	_, err := executeCommand("dev", "gen-docs", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Look for a dev-related doc file.
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), "dev") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a 'dev' doc file to be generated")
	}
}

func TestDevGenDocs_JSONOutput(t *testing.T) {
	dir := t.TempDir()
	out, err := executeCommand("--output", "json", "dev", "gen-docs", "--dir", dir)
	if err != nil {
		t.Fatalf("unexpected error: %v\nout: %s", err, out)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output not valid JSON: %v\nout: %s", err, out)
	}
	if result["dir"] != dir {
		t.Errorf("expected dir=%s, got: %v", dir, result["dir"])
	}
}

// ─── slug regex ──────────────────────────────────────────────────────────────

func TestSlugRE(t *testing.T) {
	valid := []string{"a", "abc", "my-plugin", "tech123", "a-b-c", "z9"}
	for _, s := range valid {
		if !slugRE.MatchString(s) {
			t.Errorf("expected slug %q to be valid", s)
		}
	}

	invalid := []string{"", "Bad", "bad slug", "bad_slug", "-bad", "bad-", "Bad-Slug", "123!"}
	for _, s := range invalid {
		if slugRE.MatchString(s) {
			t.Errorf("expected slug %q to be invalid", s)
		}
	}
}
