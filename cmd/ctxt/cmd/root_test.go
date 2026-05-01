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

func TestMain(m *testing.M) {
	// Disable clipboard access for all tests — avoids non-deterministic behaviour
	// when tests run with clipboard content present.
	os.Setenv("CTXT_NO_CLIPBOARD", "1")
	// Force env secrets backend so tests are not affected by the local config
	// file (which may configure keychain or another backend).
	os.Setenv("CTXT_SECRETS_BACKEND", "env")
	os.Exit(m.Run())
}

// resetAllFlags resets all flags on a command and its subcommands to defaults.
func resetAllFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		resetFlag(f)
		f.Changed = false
	})
	cmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		resetFlag(f)
		f.Changed = false
	})
	for _, sub := range cmd.Commands() {
		resetAllFlags(sub)
	}
}

func resetFlag(f *pflag.Flag) {
	if slice, ok := f.Value.(pflag.SliceValue); ok {
		_ = slice.Replace(nil)
		return
	}
	_ = f.Value.Set(f.DefValue)
}

// executeCommand runs a cobra command with args and captures all output
// (both cobra output and fmt.Print* calls to os.Stdout).
func executeCommand(args ...string) (string, error) {
	// Reset all flags to their defaults to avoid state leakage between tests.
	resetAllFlags(rootCmd)
	// Reset viper state too — kit/cli + previous BindPFlag bindings persist
	// the last --output/--format value across tests otherwise. Use the helper
	// that resets only the keys we manage, leaving "config" alone so that
	// the --config flag parsed for each call still reaches initConfig.
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "profile", "profile.default", "offline", "offline.enabled", "instance"} {
		viper.Set(k, "")
	}

	// Capture os.Stdout — drain concurrently to avoid pipe-buffer deadlock
	// when commands emit large output (e.g. bash completion scripts).
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
	// Tool-specific globals (added via kit cli.Globals).
	for _, flag := range []string{"--config", "--profile", "--offline", "--instance"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain ctxt global flag %s", flag)
		}
	}
	// Kit/cli built-ins we expect for free.
	for _, flag := range []string{"--format", "--quiet", "--no-color", "--verbose"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain kit/cli built-in flag %s", flag)
		}
	}
}

func TestRootVerboseIsCount(t *testing.T) {
	// kit/cli registers -V/--verbose as a stackable Count flag, not a bool.
	flag := rootCmd.PersistentFlags().Lookup("verbose")
	if flag == nil {
		t.Fatal("--verbose flag should be registered on root")
	}
	if flag.Value.Type() != "count" {
		t.Errorf("verbose flag should be Count type for stackable -VV; got %s", flag.Value.Type())
	}
}

func TestRootHasFormat(t *testing.T) {
	// Kit owns --format; --output is a deprecated hidden alias kept for back-compat.
	if rootCmd.PersistentFlags().Lookup("format") == nil {
		t.Error("--format flag should be provided by kit/cli")
	}
	outFlag := rootCmd.PersistentFlags().Lookup("output")
	if outFlag == nil {
		t.Error("--output should be retained as a hidden deprecated alias")
	} else if !outFlag.Hidden {
		t.Error("--output should be hidden in --help (deprecated alias)")
	}
}

func TestRootSubcommands(t *testing.T) {
	out, err := executeCommand("--help")
	if err != nil {
		t.Fatalf("root --help should succeed: %v", err)
	}
	for _, subcmd := range []string{"analyze", "import", "list", "find", "show", "delete", "edit", "compose", "config", "profile", "entity", "registry", "completion"} {
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
