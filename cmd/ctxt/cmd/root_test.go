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

// TestMain runs the package isolated from the developer's machine: HOME and
// XDG dirs point at a throwaway tree (so neither in-process commands nor the
// binaries e2e tests spawn read the real config, state or data), routing env
// is cleared, the default server.url is a closed port, and any request to the
// default local server ports is refused and fails the run. See
// internal/testguard. Without this, a test that configures no server — or a
// red run whose routing falls through to the default — posts into whatever
// real ctxt server listens on 127.0.0.1:8080.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, func() {
		// Disable clipboard access for all tests — avoids non-deterministic
		// behaviour when tests run with clipboard content present.
		os.Setenv("CTXT_NO_CLIPBOARD", "1")
		// Force env secrets backend so tests are not affected by a config
		// that configures keychain or another backend.
		os.Setenv("CTXT_SECRETS_BACKEND", "env")
		// Keep embedding calls off the developer's real Ollama: an
		// unconfigured run embeds against a closed port and degrades to
		// FTS. Tests that need a provider set their own (flag or env).
		os.Setenv("CTXT_EMBEDDING_ENDPOINT", testguard.ClosedServerURL)
	}, "ctxt"))
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
	// Tool-specific globals (added via kit cli.Globals) that are user-facing.
	// kit v0.5 hides -c/--config (it became a repeatable key=value / extra-file
	// override flag, an implementation detail), so it no longer appears in
	// --help and is asserted separately in TestRootHasFormat.
	for _, flag := range []string{"--profile", "--offline", "--instance"} {
		if !strings.Contains(out, flag) {
			t.Errorf("help output should contain ctxt global flag %s", flag)
		}
	}
	// Kit/cli built-ins we expect for free. --format is the parity-contract
	// output flag; --output is hidden in v0.5 (implementation detail).
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
