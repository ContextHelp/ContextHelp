package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// resetAllFlags resets all flags on a command and its subcommands to defaults.
func resetAllFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	for _, sub := range cmd.Commands() {
		resetAllFlags(sub)
	}
}

// executeCommand runs a cobra command with args and captures all output
// (both cobra output and fmt.Print* calls to os.Stdout).
func executeCommand(args ...string) (string, error) {
	// Reset all flags to their defaults to avoid state leakage between tests.
	resetAllFlags(rootCmd)

	// Capture os.Stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs(args)
	rootCmd.SetOut(w)
	rootCmd.SetErr(w)
	err := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	return string(out), err
}

func TestRootHelp(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	if !strings.Contains(out, "ctxt") {
		t.Error("help output should contain 'ctxt'")
	}
	if !strings.Contains(out, "ContextHelp") {
		t.Error("help output should mention ContextHelp")
	}
}

func TestRootGlobalFlags(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	for _, flag := range []string{"--config", "--profile", "--output"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain global flag %s", flag)
		}
	}
}

func TestRootSubcommands(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"analyze", "list", "find", "open", "delete", "edit", "make", "config", "profile", "jobs", "entities", "registry", "version", "completion"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("help output should list subcommand %q", subcmd)
		}
	}
}
