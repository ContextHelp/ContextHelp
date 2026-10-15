package cmd

import (
	"bytes"
	"os"
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

// A completion script that could not be written must fail the command:
// `dpkms completion zsh > file` on a full disk or a closed pipe otherwise
// exits 0 with a truncated script.
func TestCompletionWriteErrorFails(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			resetAllFlags(rootCmd)
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			_ = r.Close()
			_ = w.Close() // every write to w now fails with os.ErrClosed

			oldStdout := os.Stdout
			os.Stdout = w
			defer func() { os.Stdout = oldStdout }()

			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetErr(&buf)
			rootCmd.SetArgs([]string{"completion", shell})
			if err := rootCmd.Execute(); err == nil {
				t.Fatalf("completion %s: want write error, got nil", shell)
			}
		})
	}
}
