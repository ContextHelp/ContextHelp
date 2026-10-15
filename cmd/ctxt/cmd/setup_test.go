package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/kit/go/console/output"
)

func TestSetupNonInteractive(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)

	out, err := executeCommand("setup", "--non-interactive")
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

func TestSetupNonInteractiveViaCI(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "true")

	out, err := executeCommand("setup")
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

func TestSetupWritesValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)

	_, err := executeCommand("setup", "--non-interactive")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	content := string(data)
	// Expect key fields written.
	for _, want := range []string{"storage:", "sqlite"} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
	// No schema version stamp: config carries no version key.
	if strings.HasPrefix(content, "version:") || strings.Contains(content, "\nversion:") {
		t.Errorf("config carries a top-level version key:\n%s", content)
	}
}

func TestSetupDefaultAnswers(t *testing.T) {
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

func TestSetupHelp(t *testing.T) {
	out, err := executeCommand("setup", "--help")
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

// executeSetupWithInput runs `ctxt setup` with in piped to the
// command's stdin, restoring the shared root's input afterwards. A real
// pipe, not a strings.Reader: `printf ... | ctxt setup` hands the
// command an *os.File pipe, and pipe detection keys on exactly that.
func executeSetupWithInput(t *testing.T, in string, args ...string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(in); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	rootCmd.SetIn(r)
	t.Cleanup(func() {
		rootCmd.SetIn(nil)
		_ = r.Close()
	})
	return executeCommand(append([]string{"setup"}, args...)...)
}

// Every piped answer must reach its own question: a reader that buffers
// past the first newline and is then dropped loses the rest, and the
// later questions silently take their defaults.
func TestSetupPipedAnswersEachLand(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "")
	storage := filepath.Join(dir, "store")

	// provider=anthropic, key, storage path, pipeline=text
	out, err := executeSetupWithInput(t, "2\nsk-test\n"+storage+"\n1\n")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"backend: anthropic",
		filepath.Join(storage, "db.sqlite"),
		filepath.Join(storage, "blobs"),
		"pipeline: text.short",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
	for _, want := range []string{"Storage path: " + storage, "Pipeline:     text", "AI provider:  anthropic"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

// The overwrite confirmation shares the wizard's input: the answers
// piped after "y" must still reach the questions that follow.
func TestSetupPipedAnswersAfterOverwriteConfirm(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "")
	if err := os.WriteFile(cfgPath, []byte("# keep me\nlog:\n  level: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := filepath.Join(dir, "store")

	out, err := executeSetupWithInput(t, "y\n3\n"+storage+"\n2\n")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	for _, want := range []string{"# keep me", "level: debug", filepath.Join(storage, "db.sqlite"), "pipeline: url.basic"} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q:\n%s", want, content)
		}
	}
}

// Declining the overwrite leaves the file untouched.
func TestSetupDeclineOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "")
	const orig = "# keep me\n"
	if err := os.WriteFile(cfgPath, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := executeSetupWithInput(t, "n\n")
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Aborted.") {
		t.Errorf("expected Aborted., got:\n%s", out)
	}
	data, _ := os.ReadFile(cfgPath)
	if string(data) != orig {
		t.Errorf("config changed after decline:\n%s", data)
	}
}

// Input that ends before the last question is an error, not a run that
// quietly fills the rest with defaults; nothing is written.
func TestSetupInputEndsEarly(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	t.Setenv("CTXT_CONFIG", cfgPath)
	t.Setenv("CI", "")

	out, err := executeSetupWithInput(t, "3\n")
	var oe *output.Error
	if !errors.As(err, &oe) || oe.ExitCode != output.ExitUsage || !strings.Contains(oe.Message, "nothing written") {
		t.Fatalf("want usage error for truncated input, got %v\n%s", err, out)
	}
	if _, statErr := os.Stat(cfgPath); !os.IsNotExist(statErr) {
		t.Errorf("config written despite aborted setup: %v", statErr)
	}
}
