package providers

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

func TestLookupToolFound(t *testing.T) {
	// "echo" should exist on all Unix systems
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	path, err := LookupTool("echo")
	if err != nil {
		t.Fatalf("LookupTool(echo): %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path for echo")
	}
}

func TestLookupToolNotFound(t *testing.T) {
	_, err := LookupTool("nonexistent-tool-xyz-12345")
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
	var toolErr *ToolNotInstalledError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *ToolNotInstalledError, got %T", err)
	}
	if toolErr.Tool != "nonexistent-tool-xyz-12345" {
		t.Errorf("Tool: got %q", toolErr.Tool)
	}
}

func TestLookupToolHint(t *testing.T) {
	// Test that known tools produce install hints
	_, err := LookupTool("ffmpeg")
	if err == nil {
		t.Skip("ffmpeg is installed, cannot test hint")
	}
	var toolErr *ToolNotInstalledError
	if !errors.As(err, &toolErr) {
		t.Fatalf("expected *ToolNotInstalledError, got %T", err)
	}
	if toolErr.Hint == "" {
		t.Error("expected non-empty hint for ffmpeg")
	}
}

func TestRunCommandSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	ctx := context.Background()
	result, err := RunCommand(ctx, "echo", "hello", "world")
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if result.Stdout != "hello world\n" {
		t.Errorf("Stdout: got %q", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode: got %d", result.ExitCode)
	}
}

func TestRunCommandFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	ctx := context.Background()
	_, err := RunCommand(ctx, "false")
	if err == nil {
		t.Fatal("expected error from 'false' command")
	}
}

func TestRunCommandTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test requires Unix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := RunCommand(ctx, "sleep", "10")
	if err == nil {
		t.Fatal("expected error from timeout")
	}
}

func TestToolNotInstalledErrorMessage(t *testing.T) {
	err := &ToolNotInstalledError{Tool: "ffmpeg", Hint: "brew install ffmpeg"}
	msg := err.Error()
	if msg != "ffmpeg not found in PATH. Install: brew install ffmpeg" {
		t.Errorf("unexpected message: %q", msg)
	}

	err2 := &ToolNotInstalledError{Tool: "foo"}
	msg2 := err2.Error()
	if msg2 != "foo not found in PATH" {
		t.Errorf("unexpected message: %q", msg2)
	}
}
