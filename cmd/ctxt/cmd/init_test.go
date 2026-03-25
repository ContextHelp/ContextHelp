package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitNonInteractive(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)

	out, err := executeCommand("init", "--non-interactive")
	if err != nil {
		t.Fatalf("init --non-interactive failed: %v", err)
	}

	if !strings.Contains(out, "Configuration written successfully") {
		t.Errorf("expected success message, got: %s", out)
	}
	if !strings.Contains(out, cfgPath) {
		t.Errorf("expected config path in output, got: %s", out)
	}

	// Config file must exist.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file not created at %s: %v", cfgPath, err)
	}
}

func TestInitNonInteractiveViaCI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "true")

	out, err := executeCommand("init")
	if err != nil {
		t.Fatalf("init with CI=true failed: %v", err)
	}

	if !strings.Contains(out, "Configuration written successfully") {
		t.Errorf("expected success message, got: %s", out)
	}

	if _, err := os.Stat(cfgPath); err != nil {
		t.Errorf("config file not created at %s: %v", cfgPath, err)
	}

	// Cleanup CI env so it doesn't bleed into other tests.
	t.Cleanup(func() { os.Unsetenv("CI") })
}

func TestInitWritesValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)

	_, err := executeCommand("init", "--non-interactive")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	// Expect key fields written.
	for _, want := range []string{"version:", "storage:", "sqlite"} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
}

func TestInitDefaultAnswers(t *testing.T) {
	a := defaultAnswers()
	if a.Provider != "skip" {
		t.Errorf("default provider should be skip, got %s", a.Provider)
	}
	if a.Pipeline != "auto" {
		t.Errorf("default pipeline should be auto, got %s", a.Pipeline)
	}
	if !strings.Contains(a.StoragePath, "ctxt") {
		t.Errorf("default storage path should contain 'ctxt', got %s", a.StoragePath)
	}
}

func TestInitHelp(t *testing.T) {
	out, err := executeCommand("init", "--help")
	if err != nil {
		t.Fatalf("init --help failed: %v", err)
	}
	if !strings.Contains(out, "non-interactive") {
		t.Error("help should mention --non-interactive flag")
	}
	if !strings.Contains(out, "wizard") {
		t.Error("help should mention wizard")
	}
}

func TestPipelineToInboxValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{"text", "text.short"},
		{"url", "url.basic"},
		{"auto", "auto"},
		{"", "auto"},
	}
	for _, c := range cases {
		got := pipelineToInboxValue(c.in)
		if got != c.want {
			t.Errorf("pipelineToInboxValue(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
