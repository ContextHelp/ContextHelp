package cmd

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// newUnknownSubcommandTree builds a miniature stand-in for ctxt's
// shape: a root with a non-runnable group carrying one runnable leaf,
// which is exactly the arrangement cobra answers with help and exit 0.
func newUnknownSubcommandTree() *cobra.Command {
	root := &cobra.Command{Use: "ctxt", RunE: func(*cobra.Command, []string) error { return nil }}
	root.SuggestionsMinimumDistance = 2

	group := &cobra.Command{Use: "profile"} // non-runnable on purpose
	group.AddCommand(&cobra.Command{
		Use:  "list",
		RunE: func(*cobra.Command, []string) error { return nil },
	})
	root.AddCommand(group)
	return root
}

// TestCheckUnknownSubcommand_RefusesTypoUnderGroup is the regression
// guard for the worst failure this file exists to stop: `ctxt profile
// bogus` printed the group's help and exited 0, so an automated
// caller recorded a typo as a completed step.
func TestCheckUnknownSubcommand_RefusesTypoUnderGroup(t *testing.T) {
	t.Parallel()

	err := checkUnknownSubcommand(newUnknownSubcommandTree(), []string{"profile", "bogus"})
	if err == nil {
		t.Fatal("unknown subcommand under a group was accepted; it must be refused")
	}
	if got := ExitCodeFor(err); got != output.ExitUsage {
		t.Fatalf("ExitCodeFor = %d, want %d (USAGE)", got, output.ExitUsage)
	}
}

// TestCheckUnknownSubcommand_Allows pins everything the check must
// stay silent about, so closing the gap does not start refusing
// invocations cobra handles correctly on its own.
func TestCheckUnknownSubcommand_Allows(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{"bare group renders help", []string{"profile"}},
		{"resolved runnable leaf", []string{"profile", "list"}},
		{"leaf with operands is its own Args rule", []string{"profile", "list", "extra"}},
		{"help flag ahead of the word", []string{"profile", "--help", "bogus"}},
		{"leading help command", []string{"help", "profile"}},
		{"empty argv", nil},
		{"root level typo is cobra's to report", []string{"bogus"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := checkUnknownSubcommand(newUnknownSubcommandTree(), tc.args); err != nil {
				t.Fatalf("checkUnknownSubcommand(%v) = %v, want nil", tc.args, err)
			}
		})
	}
}

// TestCheckUnknownSubcommand_TrailingHelpFlagIsTreatedAsHelp records
// measured behavior, not an endorsement of it.
//
// Kit's own implementation documents cobra's positional rule: a help
// flag BEHIND the unknown word should not rescue it, because cobra
// only reaches its help check once Find has routed. This check does
// not reproduce that, and the divergence is structural rather than an
// oversight: strippedWords parses the leftovers against a flag set
// that includes a registered --help, so pflag answers ErrHelp and the
// check bails at the help branch before helpFlagPrecedes is ever
// consulted. Kit hits the same branch for the same reason.
//
// Left as-is deliberately. The behavior is exit 0 on a help request,
// which is what a help request should do; making it refuse would risk
// rejecting legitimate `--help` invocations, and the class this
// change exists to fix — the exit code of an actual failure — is
// unaffected. Pinned here so the day it changes is a decision rather
// than a surprise.
func TestCheckUnknownSubcommand_TrailingHelpFlagIsTreatedAsHelp(t *testing.T) {
	t.Parallel()

	if err := checkUnknownSubcommand(newUnknownSubcommandTree(), []string{"profile", "bogus", "--help"}); err != nil {
		t.Fatalf("trailing --help = %v, want nil (treated as a help request)", err)
	}
}

// TestCheckUnknownSubcommand_RefusalIsUsageEnvelope pins that the
// refusal is a typed kit envelope rather than a bare error, so every
// surface reading the taxonomy off it agrees on USAGE.
func TestCheckUnknownSubcommand_RefusalIsUsageEnvelope(t *testing.T) {
	t.Parallel()

	err := checkUnknownSubcommand(newUnknownSubcommandTree(), []string{"profile", "bogus"})
	var env *output.Error
	if !errors.As(err, &env) {
		t.Fatalf("refusal is not an *output.Error: %#v", err)
	}
	if env.Code != output.CodeUsage {
		t.Fatalf("Code = %q, want %q", env.Code, output.CodeUsage)
	}
}
