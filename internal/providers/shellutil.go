package providers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ToolNotInstalledError indicates a required CLI tool is not on PATH.
type ToolNotInstalledError struct {
	Tool string
	Hint string
}

func (e *ToolNotInstalledError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s not found in PATH. Install: %s", e.Tool, e.Hint)
	}
	return fmt.Sprintf("%s not found in PATH", e.Tool)
}

// installHints returns platform-specific install instructions.
var installHints = map[string]map[string]string{
	"ffmpeg": {
		"darwin": "brew install ffmpeg",
		"linux":  "apt install ffmpeg   # or: dnf install ffmpeg",
	},
	"ffprobe": {
		"darwin": "brew install ffmpeg",
		"linux":  "apt install ffmpeg   # or: dnf install ffmpeg",
	},
	"pdftotext": {
		"darwin": "brew install poppler",
		"linux":  "apt install poppler-utils",
	},
	"pdfinfo": {
		"darwin": "brew install poppler",
		"linux":  "apt install poppler-utils",
	},
	"tesseract": {
		"darwin": "brew install tesseract",
		"linux":  "apt install tesseract-ocr",
	},
	"whisper": {
		"darwin": "brew install whisper-cpp   # or build from source: github.com/ggerganov/whisper.cpp",
		"linux":  "build from source: github.com/ggerganov/whisper.cpp",
	},
	"pyannote": {
		"darwin": "pip install pyannote.audio",
		"linux":  "pip install pyannote.audio",
	},
}

// LookupTool checks whether a CLI tool is available on PATH or common local paths.
// Returns the full path or a ToolNotInstalledError with install hints.
func LookupTool(name string) (string, error) {
	// 1. Try standard PATH
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// 2. Try common local paths
	home, _ := os.UserHomeDir()
	commonPaths := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
	}

	for _, p := range commonPaths {
		fullPath := filepath.Join(p, name)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath, nil
		}
	}

	hint := ""
	if hints, ok := installHints[name]; ok {
		hint = hints[runtime.GOOS]
	}
	return "", &ToolNotInstalledError{Tool: name, Hint: hint}
}

// CommandResult holds the output of a shell command.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// RunCommand executes a CLI tool with the given arguments, respecting context cancellation and timeout.
func RunCommand(ctx context.Context, name string, args ...string) (*CommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- caller controls command name and args

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &CommandResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("%s exited with code %d: %s", name, result.ExitCode, strings.TrimSpace(result.Stderr))
		}
		return result, fmt.Errorf("%s: %w", name, err)
	}

	return result, nil
}
