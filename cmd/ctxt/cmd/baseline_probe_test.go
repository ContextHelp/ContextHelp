// Package cmd: T-0592 baseline probe.
//
// Build-tag gated so the file is invisible to the normal `go test ./...`
// run. Invoke explicitly:
//
//	go test -tags=ctxtbaselineprobe -run TestBaselineProbe -v ./cmd/ctxt/cmd
//
// The probe walks the constructed root command tree and dumps the
// kit/cli ValidationError + SignatureReport in JSON so T-0592 can
// bucket failures. It also re-runs Validate against a copy of the
// kit Config with EnforceGuidance / EnforceDryRunRationale /
// EnforceDestructiveToken / SignatureStrictness=reject enabled so
// the baseline captures the post-T-0595 strict-gate picture.
//
//go:build ctxtbaselineprobe

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// TestBaselineProbe is not a real test; it dumps validator state so
// the conformance baseline doc can enumerate failure buckets.
func TestBaselineProbe(t *testing.T) {
	// Force every subcommand init() to run by importing this package
	// (the act of compiling pulls them in). Ensure groups + reserved
	// names are stitched the same way Execute() does.
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	// Match Execute()'s order: shape annotations BEFORE Validate so
	// the probe sees the validator state after foundation wiring.
	applyShapeAnnotations()

	// Default (kit-shipped 12fcc-leak) Validate.
	verr := root.Validate()
	if verr != nil {
		fmt.Fprintln(os.Stderr, "=== Validate() default-gates ===")
		fmt.Fprintln(os.Stderr, verr.Error())
	} else {
		fmt.Fprintln(os.Stderr, "=== Validate() default-gates: PASS (no issues) ===")
	}

	// SignatureReport (silent by default — surface anyway).
	report := root.ValidateSignature()
	fmt.Fprintln(os.Stderr, "=== ValidateSignature() default-gates ===")
	_ = report.RenderText(os.Stderr)

	// JSON dump of the full validation-error struct so T-0592 can
	// bucket each missing item.
	if ve, ok := verr.(*kitcli.ValidationError); ok && ve != nil {
		dumpStruct(t, "validation-error.default-gates", ve)
	}
	dumpStruct(t, "signature-report.default-gates", report)

	// Probe with the stricter gates flipped on. Construct a separate
	// Root with the same identity + groups so we can re-Validate
	// without disturbing the canonical root.
	strictRoot := kitcli.New(kitcli.Config{
		Name:                    "ctxt",
		Version:                 "dev",
		Short:                   "ContextHelp - Your agentic context brain",
		EnforceGuidance:         true,
		EnforceDryRunRationale:  true,
		EnforceDestructiveToken: true,
		SignatureStrictness:     kitcli.SignatureStrictnessReject,
		PassthroughStrictness:   kitcli.PassthroughReject,
		// Mirror the live root's expanded depth-1 surface so the
		// strict probe doesn't false-positive on the cap rule.
		MaxTopLevelVerbs: 30,
	})
	// Re-parent every top-level subcommand off the canonical root
	// into strictRoot.Cmd so the validator sees the same tree.
	for _, c := range rootCmd.Commands() {
		if c.Name() == "completion" || c.Name() == "help" {
			continue
		}
		// cobra.Command.AddCommand re-parents.
		strictRoot.Cmd.AddCommand(c)
	}
	strictErr := strictRoot.Validate()
	if strictErr != nil {
		fmt.Fprintln(os.Stderr, "=== Validate() strict-gates ===")
		fmt.Fprintln(os.Stderr, strictErr.Error())
		if ve, ok := strictErr.(*kitcli.ValidationError); ok {
			dumpStruct(t, "validation-error.strict-gates", ve)
		}
	} else {
		fmt.Fprintln(os.Stderr, "=== Validate() strict-gates: PASS ===")
	}
	strictReport := strictRoot.ValidateSignature()
	dumpStruct(t, "signature-report.strict-gates", strictReport)

	// Enumerate cmd-tree roots (for fan-out planning).
	fmt.Fprintln(os.Stderr, "=== top-level commands ===")
	for _, c := range rootCmd.Commands() {
		runnable := c.Runnable()
		flag := "group"
		if runnable {
			flag = "leaf"
		}
		fmt.Fprintf(os.Stderr, "  %-16s %s\n", c.Name(), flag)
	}
}

func dumpStruct(t *testing.T, label string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: marshal error: %v\n", label, err)
		return
	}
	fmt.Fprintf(os.Stderr, "--- %s ---\n%s\n", label, b)
}

// Silence unused-import warnings if cobra ends up unreferenced after edits.
var _ = (*cobra.Command)(nil)
