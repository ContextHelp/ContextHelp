package providers

import (
	"bytes"
	"context"
	"errors"
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

// CLI tools the providers run, by executable name.
const (
	toolFFmpeg    = "ffmpeg"
	toolTesseract = "tesseract"
)

// installHints returns platform-specific install instructions.
var installHints = map[string]map[string]string{
	toolFFmpeg: {
		"darwin": "brew install ffmpeg",
		"linux":  "apt install ffmpeg   # or: dnf install ffmpeg",
	},
	"ffprobe": {
		"darwin": "brew install ffmpeg",
		"linux":  "apt install ffmpeg   # or: dnf install ffmpeg",
	},
	pdftotextTool: {
		"darwin": "brew install poppler",
		"linux":  "apt install poppler-utils",
	},
	"pdfinfo": {
		"darwin": "brew install poppler",
		"linux":  "apt install poppler-utils",
	},
	toolTesseract: {
		"darwin": "brew install tesseract",
		"linux":  "apt install tesseract-ocr",
	},
	backendWhisper: {
		"darwin": "brew install whisper-cpp   # installs the whisper-cli binary",
		"linux":  "build from source: github.com/ggerganov/whisper.cpp (binary whisper-cli)",
	},
	pyannoteName: {
		"darwin": "pip install pyannote.audio",
		"linux":  "pip install pyannote.audio",
	},
}

// ToolLookup resolves a CLI tool name to a path, or errors when the tool is
// unavailable.
type ToolLookup func(name string) (string, error)

type toolLookupKey struct{}

// WithToolLookup returns a context whose LookupToolContext calls go through
// l instead of probing PATH. Tests that replay recorded tool output use it
// so availability probes do not depend on the machine.
func WithToolLookup(ctx context.Context, l ToolLookup) context.Context {
	return context.WithValue(ctx, toolLookupKey{}, l)
}

// IsCommandNotRecorded reports whether err says the injected
// CommandRunner had no recording for the command (an error in the chain
// with a CommandNotRecorded() bool method returning true). Providers that
// retry a failed command with other flags must not retry on it: a replay
// miss is a test setup error, not a flag mismatch.
func IsCommandNotRecorded(err error) bool {
	var n interface{ CommandNotRecorded() bool }
	return errors.As(err, &n) && n.CommandNotRecorded()
}

// ExtraToolDirs lists the directories searched for tools after PATH.
func ExtraToolDirs() []string { return extraToolDirs() }

// extraToolDirs backs ExtraToolDirs and LookupTool. A variable so tests
// can make a missing tool missing on any machine.
var extraToolDirs = func() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		filepath.Join(home, ".local", "bin"),
	}
}

// LookupTool checks whether a CLI tool is available on PATH or common local paths.
// Returns the full path or a ToolNotInstalledError with install hints.
func LookupTool(name string) (string, error) {
	return LookupToolContext(context.Background(), name)
}

// LookupToolContext is LookupTool honouring a [WithToolLookup] seam in ctx.
func LookupToolContext(ctx context.Context, name string) (string, error) {
	if l, ok := ctx.Value(toolLookupKey{}).(ToolLookup); ok && l != nil {
		return l(name)
	}
	// 1. Try standard PATH
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}

	// 2. Try common local paths
	for _, p := range extraToolDirs() {
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

// CommandRunner runs one CLI tool call and reports its output. The error
// is non-nil when the tool cannot start or exits non-zero; exitCode carries
// the process exit code either way (-1 when the process never ran).
type CommandRunner func(ctx context.Context, name string, args ...string) (stdout, stderr string, exitCode int, err error)

type commandRunnerKey struct{}

// WithCommandRunner returns a context whose RunCommand calls go through r
// instead of starting a process. It is the seam tests use to replay
// recorded tool output; production code never sets it.
func WithCommandRunner(ctx context.Context, r CommandRunner) context.Context {
	return context.WithValue(ctx, commandRunnerKey{}, r)
}

// RawCommandResult is CommandResult with stdout kept as the bytes the
// tool wrote, for binary output (images) that must not be copied again.
type RawCommandResult struct {
	Stdout   []byte
	Stderr   string
	ExitCode int
}

// RunCommandRaw is RunCommand returning stdout as bytes.
func RunCommandRaw(ctx context.Context, name string, args ...string) (*RawCommandResult, error) {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 -- caller controls command name and args

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &RawCommandResult{Stdout: stdout.Bytes(), Stderr: stderr.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("%s exited with code %d: %s", name, result.ExitCode, strings.TrimSpace(result.Stderr))
		}
		return result, fmt.Errorf("%s: %w", name, err)
	}
	return result, nil
}

// RunCommand executes a CLI tool with the given arguments, respecting context cancellation and timeout.
func RunCommand(ctx context.Context, name string, args ...string) (*CommandResult, error) {
	if r, ok := ctx.Value(commandRunnerKey{}).(CommandRunner); ok && r != nil {
		stdout, stderr, code, err := r(ctx, name, args...)
		result := &CommandResult{Stdout: stdout, Stderr: stderr, ExitCode: code}
		if err != nil {
			if code > 0 {
				return result, fmt.Errorf("%s exited with code %d: %s", name, code, strings.TrimSpace(stderr))
			}
			return result, fmt.Errorf("%s: %w", name, err)
		}
		return result, nil
	}

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
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("%s exited with code %d: %s", name, result.ExitCode, strings.TrimSpace(result.Stderr))
		}
		return result, fmt.Errorf("%s: %w", name, err)
	}

	return result, nil
}

// roleTools names the CLI tool behind each tool-backed provider role.
var roleTools = map[string]string{
	"ocr":           toolTesseract,
	"video":         toolFFmpeg,
	"frames":        toolFFmpeg,
	"transcription": backendWhisper,
	"diarization":   pyannoteName,
	"document":      pdftotextTool,
}

// InstallHint says what to install or configure for a provider role, or
// "" for an unknown role.
func InstallHint(role string) string {
	if role == "vision" {
		return "run a local vision model with Ollama (ollama pull <model>) and set providers.vision.model, or name a vision backend in providers.vision.backend"
	}
	tool, ok := roleTools[role]
	if !ok {
		return ""
	}
	hint := installHints[tool][runtime.GOOS]
	if hint == "" {
		hint = "install " + tool
	}
	if role == "transcription" {
		hint += "; then set providers.transcription.model to a whisper.cpp model file"
	}
	return hint
}
