// Package cmd: T-0595 strict-gate regression guard.
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
