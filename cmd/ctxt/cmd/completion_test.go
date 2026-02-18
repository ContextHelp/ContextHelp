package cmd

import (
	"strings"
	"testing"
)

func TestCompletionBash(t *testing.T) {
	out, err := executeCommand("completion", "bash")
	if err != nil {
		t.Fatalf("completion bash should succeed: %v", err)
	}
	if !strings.Contains(out, "bash") {
		t.Error("output should contain bash completion script")
	}
}

func TestCompletionZsh(t *testing.T) {
	out, err := executeCommand("completion", "zsh")
	if err != nil {
		t.Fatalf("completion zsh should succeed: %v", err)
	}
	if !strings.Contains(out, "zsh") || !strings.Contains(out, "compdef") {
		t.Error("output should contain zsh completion script")
	}
}

func TestCompletionFish(t *testing.T) {
	out, err := executeCommand("completion", "fish")
	if err != nil {
		t.Fatalf("completion fish should succeed: %v", err)
	}
	if !strings.Contains(out, "fish") || !strings.Contains(out, "complete") {
		t.Error("output should contain fish completion script")
	}
}

func TestCompletionPowershell(t *testing.T) {
	out, err := executeCommand("completion", "powershell")
	if err != nil {
		t.Fatalf("completion powershell should succeed: %v", err)
	}
	if !strings.Contains(out, "Register-ArgumentCompleter") {
		t.Error("output should contain powershell completion script")
	}
}

func TestCompletionInvalidShellError(t *testing.T) {
	_, err := executeCommand("completion", "ksh")
	if err == nil {
		t.Error("completion with invalid shell should fail")
	}
}

func TestCompletionNoShellError(t *testing.T) {
	_, err := executeCommand("completion")
	if err == nil {
		t.Error("completion without shell argument should fail")
	}
}
