package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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
// (both cobra output and fmt.Print* calls to os.Stdout). Drains the pipe
// concurrently to avoid deadlock on large output (e.g. completion scripts).
func executeCommand(args ...string) (string, error) {
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "offline", "offline.enabled", "data-dir", "server-url", "storage.path", "server.url"} {
		viper.Set(k, "")
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs(args)
	rootCmd.SetOut(w)
	rootCmd.SetErr(w)

	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout
	out := <-done
	return string(out), err
}

func TestRootHelp(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	if !strings.Contains(out, "dpkms") {
		t.Error("help output should contain 'dpkms'")
	}
	if !strings.Contains(out, "dPKMS") {
		t.Error("help output should mention dPKMS")
	}
}

func TestRootGlobalFlags(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	for _, flag := range []string{"--config", "--data-dir", "--server-url", "--offline"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain ctxt global flag %s", flag)
		}
	}
	for _, flag := range []string{"--format", "--quiet", "--no-color", "--verbose"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain kit/cli built-in flag %s", flag)
		}
	}
}

func TestRootVerboseIsCount(t *testing.T) {
	flag := rootCmd.PersistentFlags().Lookup("verbose")
	if flag == nil {
		t.Fatal("--verbose should be registered")
	}
	if flag.Value.Type() != "count" {
		t.Errorf("verbose should be Count for stackable -VV; got %s", flag.Value.Type())
	}
}

func TestRootHasFormat(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("format") == nil {
		t.Error("--format flag should be provided by kit/cli")
	}
	outFlag := rootCmd.PersistentFlags().Lookup("output")
	if outFlag == nil {
		t.Error("--output should be retained as hidden deprecated alias")
	} else if !outFlag.Hidden {
		t.Error("--output should be hidden in help (deprecated alias)")
	}
}

func TestRootSubcommands(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"serve", "housekeeping", "completion", "dev", "detector", "job", "secret", "key"} {
		if !strings.Contains(out, subcmd) {
			t.Errorf("help output should list subcommand %q", subcmd)
		}
	}
}

func TestRootSuggestionsEnabled(t *testing.T) {
	if rootCmd.DisableSuggestions {
		t.Error("cobra suggestions must not be disabled on rootCmd")
	}
	if rootCmd.SuggestionsMinimumDistance != 2 {
		t.Errorf("SuggestionsMinimumDistance want 2, got %d", rootCmd.SuggestionsMinimumDistance)
	}
}
