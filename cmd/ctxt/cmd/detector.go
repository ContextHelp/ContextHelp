package cmd

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var detectorCmd = &cobra.Command{
	Use:        "detector",
	Short:      "[deprecated] moved to dpkms detector",
	Long:       `Deprecated alias for the dpkms detector command group. Forwards to 'dpkms detector' subcommands.`,
	Deprecated: "use 'dpkms detector' instead",
	RunE:       movedToDpkms("detector"),
}

// detectorSideEffect maps each deprecated detector subcommand to its
// kit/side-effect class.
var detectorSideEffect = map[string]cliconv.SideEffect{
	"add":     cliconv.SideEffectWrite,
	"list":    cliconv.SideEffectRead,
	"remove":  cliconv.SideEffectDestructive,
	"enable":  cliconv.SideEffectWrite,
	"disable": cliconv.SideEffectWrite,
}

// detectorIdempotency maps each deprecated detector subcommand to its
// kit/idempotent class (kit defaults do not cover enable/disable/remove).
var detectorIdempotency = map[string]cliconv.Idempotency{
	"add":     cliconv.IdempotencyNo,
	"list":    cliconv.IdempotencyYes,
	"remove":  cliconv.IdempotencyYes,
	"enable":  cliconv.IdempotencyYes,
	"disable": cliconv.IdempotencyYes,
}

var detectorLong = map[string]string{
	"add":     "Deprecated alias for 'dpkms detector add'. Forwards invocations to dpkms.",
	"list":    "Deprecated alias for 'dpkms detector list'. Forwards invocations to dpkms.",
	"remove":  "Deprecated alias for 'dpkms detector remove'. Forwards invocations to dpkms.",
	"enable":  "Deprecated alias for 'dpkms detector enable'. Forwards invocations to dpkms.",
	"disable": "Deprecated alias for 'dpkms detector disable'. Forwards invocations to dpkms.",
}

// detectorExamples supplies 12fcc strict-gate Examples per subcommand;
// the surface is deprecated so the examples mirror the dpkms verb.
var detectorExamples = map[string][]cliconv.Example{
	"add": {
		{Title: "Add a detector", Command: "dpkms detector add my-detector --pattern '^foo'"},
		{Title: "Add and enable", Command: "dpkms detector add my-detector --pattern '^foo' --enable"},
	},
	"list": {
		{Title: "List detectors", Command: "dpkms detector list"},
		{Title: "Filter by enabled", Command: "dpkms detector list --enabled"},
	},
	"remove": {
		{Title: "Remove a detector", Command: "dpkms detector remove my-detector --confirm=yes"},
		{Title: "Prompt for confirmation", Command: "dpkms detector remove my-detector --confirm=prompt"},
	},
	"enable": {
		{Title: "Enable a detector", Command: "dpkms detector enable my-detector"},
		{Title: "Enable all detectors", Command: "dpkms detector enable --all"},
	},
	"disable": {
		{Title: "Disable a detector", Command: "dpkms detector disable my-detector"},
		{Title: "Disable all detectors", Command: "dpkms detector disable --all"},
	},
}

// detectorNextSteps supplies follow-ups for write/destructive verbs.
var detectorNextSteps = map[string][]cliconv.NextStep{
	"add": {
		{When: "on success", Suggest: "dpkms detector enable <name>", Reason: "activate the newly added detector"},
	},
	"remove": {
		{When: "after remove", Suggest: "dpkms detector list", Reason: "verify the detector no longer appears"},
	},
	"enable": {
		{When: "on success", Suggest: "dpkms detector list --enabled", Reason: "confirm the detector is now active"},
	},
	"disable": {
		{When: "on success", Suggest: "dpkms detector list", Reason: "confirm the detector is now inactive"},
	},
}

func init() {
	rootCmd.AddCommand(detectorCmd)

	for _, sub := range []string{
		"add", "list", "remove", "enable", "disable",
	} {
		name := sub
		c := &cobra.Command{
			Use:                name,
			Short:              fmt.Sprintf("[deprecated] moved to dpkms detector %s", name),
			Long:               detectorLong[name],
			Deprecated:         fmt.Sprintf("use 'dpkms detector %s' instead", name),
			RunE:               movedToDpkms("detector " + name),
			DisableFlagParsing: true,
		}
		cliconv.WithSideEffect(c, detectorSideEffect[name])
		cliconv.WithIdempotency(c, detectorIdempotency[name])
		// 12fcc strict-gate: destructive subverbs require the typed-token
		// confirmation flow. Only "remove" is destructive in this group.
		if detectorSideEffect[name] == cliconv.SideEffectDestructive {
			cliconv.WithDestructiveToken(c)
		}
		if ex, ok := detectorExamples[name]; ok {
			cliconv.WithExamples(c, ex)
		}
		if ns, ok := detectorNextSteps[name]; ok {
			cliconv.WithNextSteps(c, ns)
		}
		detectorCmd.AddCommand(c)
	}
}
