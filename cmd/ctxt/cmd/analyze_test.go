package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeWithArgument(t *testing.T) {
	out, err := executeCommand("analyze", "test insight")
	if err != nil {
		t.Fatalf("analyze with argument should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID: job_12345678") {
		t.Error("output should contain job ID")
	}
	if !strings.Contains(out, "Content preview: test insight") {
		t.Error("output should show content preview")
	}
}

func TestAnalyzeWithFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(f, []byte("file content here"), 0644); err != nil {
		t.Fatal(err)
	}

	out, err := executeCommand("analyze", "--file", f)
	if err != nil {
		t.Fatalf("analyze with --file should succeed: %v", err)
	}
	if !strings.Contains(out, "Job ID:") {
		t.Error("output should contain job ID")
	}
	if !strings.Contains(out, "Content preview: file content here") {
		t.Error("output should preview file content")
	}
}

func TestAnalyzeWithFlags(t *testing.T) {
	out, err := executeCommand("analyze", "content",
		"--type", "url",
		"--hints", "#ux #bug",
		"--mentions", "@ui.best-practice",
		"--pipeline", "url.article",
		"--lang", "fr",
		"--raw",
		"--wait",
	)
	if err != nil {
		t.Fatalf("analyze with flags should succeed: %v", err)
	}
	if !strings.Contains(out, "Type: url") {
		t.Error("output should reflect --type flag")
	}
}

func TestAnalyzeNoInputError(t *testing.T) {
	// When stdin is a terminal (no pipe) and no args/file, should error
	_, err := executeCommand("analyze")
	if err == nil {
		t.Error("analyze with no input should fail")
	}
}

func TestAnalyzeMissingFileError(t *testing.T) {
	_, err := executeCommand("analyze", "--file", "/nonexistent/file.txt")
	if err == nil {
		t.Error("analyze with missing file should fail")
	}
}

func TestAnalyzeHelp(t *testing.T) {
	out, err := executeCommand("analyze", "--help")
	if err != nil {
		t.Fatalf("analyze --help should succeed: %v", err)
	}
	for _, flag := range []string{"--type", "--file", "--hints", "--mentions", "--pipeline", "--lang", "--translate", "--raw", "--wait"} {
		if !strings.Contains(out, flag) {
			t.Errorf("analyze help should list flag %s", flag)
		}
	}
}
