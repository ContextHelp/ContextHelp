package cmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// TestMain isolates HOME and XDG dirs so host state (current-instance file,
// stored profiles, pidfiles, config) never leaks into tests, and refuses any
// request to the default local server ports: the client commands (pipeline,
// healthcheck) default to http://localhost:8080, where a developer's real
// server may listen. See internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, func() {
		// Keep embedding calls off the developer's real Ollama: an
		// unconfigured run embeds against a closed port. Tests that need
		// a provider set their own (flag or env).
		os.Setenv("CTXT_EMBEDDING_ENDPOINT", testguard.ClosedServerURL)
	}))
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

// resetFlag restores a single flag to its default. Slice/array flags (e.g.
// kit's repeatable -c/--config StringArray) must be cleared with Replace(nil):
// calling Set(DefValue) on them APPENDS the default token instead of replacing,
// so -c values accumulate across tests and a stale path leaks into later runs.
func resetFlag(f *pflag.Flag) {
	if slice, ok := f.Value.(pflag.SliceValue); ok {
		_ = slice.Replace(nil)
		return
	}
	_ = f.Value.Set(f.DefValue)
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
	// kit v0.5 hides -c/--config (repeatable key=value / extra-file override),
	// so it is no longer in --help; it is asserted separately in TestRootHasFormat.
	for _, flag := range []string{"--data-dir", "--server-url", "--offline"} {
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
	formatFlag := rootCmd.PersistentFlags().Lookup("format")
	if formatFlag == nil {
		t.Fatal("--format flag should be provided by kit/cli")
	}
	// --format is the parity-contract output flag and must stay visible.
	if formatFlag.Hidden {
		t.Error("kit/cli's --format is the parity contract flag and must NOT be hidden")
	}
	// kit v0.5 keeps --output (shorthand -o) registered as the output-path
	// flag, but hides it: --format is the sole user-facing output flag, the
	// rest of the output suite is implementation detail. (Supersedes the
	// earlier T-0457 contract where --output was user-facing.)
	outFlag := rootCmd.PersistentFlags().Lookup("output")
	if outFlag == nil {
		t.Fatal("--output should still be registered by kit/cli as the output-path flag")
	}
	if !outFlag.Hidden {
		t.Error("kit/cli's --output should be hidden in v0.5 (implementation detail behind --format)")
	}
	if outFlag.Shorthand != "o" {
		t.Errorf("kit/cli's --output shorthand: got %q, want %q", outFlag.Shorthand, "o")
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
