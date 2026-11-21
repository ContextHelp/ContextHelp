// Package cmd: strict-gate regression guard.
//
// Unlike the build-tagged baseline probe in baseline_probe_test.go,
// this test runs on every `go test ./...` invocation. It mirrors
// Execute()'s pre-flight (applyCommandGroups → ApplyGroupVisibility →
// applyShapeAnnotations → root.Validate) and asserts that
// root.Validate() returns nil under the live (now strict) Root
// config.
//
// The test exists to catch the most common 12fcc regression: a new
// subcommand registered without kit/side-effect, kit/idempotent,
// kit/examples (and kit/next-steps + kit/destructive-token where
// applicable) annotations. Adding such a command would cause
// `ctxt --help` to exit non-zero at boot for end users; this test
// fails fast in CI instead.
//
// Hermetic: no DB, no sqlite/FTS5 dependency, no network. The test
// only touches the cobra command tree assembled at package init.

package cmd

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// TestRootValidate_StrictGatesPass asserts the live Root config
// passes Validate() with all strict gates enabled. If this fails,
// inspect the returned *kitcli.ValidationError to see which leaf
// is missing which annotation, then either add the annotation via
// internal/cli/cliconv helpers or interrogate whether the new
// command really belongs in the tree.
func TestRootValidate_StrictGatesPass(t *testing.T) {
	// Sanity-check that the live config actually has the strict
	// gates flipped on. Catches accidental reverts of root.go.
	if !root.Config.EnforceValidate {
		t.Fatalf("root.Config.EnforceValidate must be true; got false")
	}
	if !root.Config.EnforceGuidance {
		t.Fatalf("root.Config.EnforceGuidance must be true; got false")
	}
	if !root.Config.EnforceDryRunRationale {
		t.Fatalf("root.Config.EnforceDryRunRationale must be true; got false")
	}
	if !root.Config.EnforceDestructiveToken {
		t.Fatalf("root.Config.EnforceDestructiveToken must be true; got false")
	}
	if root.Config.SignatureStrictness != kitcli.SignatureStrictnessReject {
		t.Fatalf("root.Config.SignatureStrictness must be %q; got %q",
			kitcli.SignatureStrictnessReject, root.Config.SignatureStrictness)
	}
	if root.Config.PassthroughStrictness != "reject" {
		t.Fatalf("root.Config.PassthroughStrictness must be %q; got %q",
			"reject", root.Config.PassthroughStrictness)
	}

	// Clear any flag state leaked from a prior test in this package: an
	// earlier executeCommand("--config", <tempfile>) call leaves the
	// repeatable -c/--config value set on rootCmd, and the strict validator
	// rejects a bare path as "not a key=value pair". A fresh boot (what end
	// users hit) has no such value, so reset before mirroring the preflight.
	resetAllFlags(rootCmd)

	// Mirror Execute()'s pre-flight order so the validator sees the
	// same tree shape end users will see.
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	applyShapeAnnotations()

	if err := root.Validate(); err != nil {
		t.Fatalf("root.Validate() must return nil under strict gates; got:\n%v", err)
	}
}

// TestRunnableGroups_DeclareSideEffect asserts every command that has
// subcommands and also runs on its own carries a kit/side-effect tier.
//
// Such a group runs like a leaf, but kit's Validate checks only
// leaves: a group without a tier is not refused at boot, it is named
// in a stderr warning on every invocation of ctxt, and discovery and
// the policy gates see a command that declares nothing. The root and
// kit-reserved verbs are skipped, as kit skips them.
func TestRunnableGroups_DeclareSideEffect(t *testing.T) {
	valid := map[kitcli.SideEffect]bool{}
	for _, s := range []kitcli.SideEffect{
		cliconv.SideEffectRead, cliconv.SideEffectWrite,
		cliconv.SideEffectWriteLocal, cliconv.SideEffectWriteShared,
		cliconv.SideEffectDestructive, cliconv.SideEffectDestructiveLocal,
		cliconv.SideEffectDestructiveShared, cliconv.SideEffectInteractive,
	} {
		valid[s] = true
	}

	var check func(cmd *cobra.Command, top string)
	check = func(cmd *cobra.Command, top string) {
		for _, sub := range cmd.Commands() {
			name := top
			if cmd == rootCmd {
				name = sub.Name()
			}
			if root.IsReserved(name) || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.HasSubCommands() && sub.Runnable() {
				if s, ok := kitcli.GetSideEffect(sub); !ok || !valid[s] {
					t.Errorf("%s runs on its own but declares no kit/side-effect tier (got %q)",
						sub.CommandPath(), string(s))
				}
			}
			check(sub, name)
		}
	}
	check(rootCmd, "")
}
