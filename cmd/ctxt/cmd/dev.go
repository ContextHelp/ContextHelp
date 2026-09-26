package cmd

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:        "dev",
	Short:      "[deprecated] moved to dpkms dev",
	Long:       `Deprecated alias for the dpkms dev command group. Forwards to 'dpkms dev' subcommands.`,
	Deprecated: "use 'dpkms dev' instead",
	RunE:       movedToDpkms("dev"),
}

// devLong supplies a per-subcommand Long description so the deprecated
// dev stubs satisfy the 12fcc cmd.Long requirement.
var devLong = map[string]string{
	"validate-registry": "Deprecated alias for 'dpkms dev validate-registry'. Forwards invocations to dpkms.",
	"init-plugin":       "Deprecated alias for 'dpkms dev init-plugin'. Forwards invocations to dpkms.",
	"gen-docs":          "Deprecated alias for 'dpkms dev gen-docs'. Forwards invocations to dpkms.",
}

// devExamples supplies 12fcc strict-gate Examples per subcommand. The
// surface is deprecated so the examples mirror the dpkms canonical CLI.
var devExamples = map[string][]cliconv.Example{
	"validate-registry": {
		{Title: "Validate the local registry", Command: "dpkms dev validate-registry"},
		{Title: "Validate a remote registry", Command: "dpkms dev validate-registry --registry https://registry.example.com"},
	},
	"init-plugin": {
		{Title: "Scaffold a new plugin", Command: "dpkms dev init-plugin my-plugin"},
		{Title: "Scaffold with template", Command: "dpkms dev init-plugin my-plugin --template detector"},
	},
	"gen-docs": {
		{Title: "Regenerate CLI docs", Command: "dpkms dev gen-docs"},
		{Title: "Write to a custom directory", Command: "dpkms dev gen-docs --out docs/cli"},
	},
}

// devNextSteps supplies follow-ups for the dev write verbs.
var devNextSteps = map[string][]cliconv.NextStep{
	"validate-registry": {
		{When: "on validation errors", Suggest: "dpkms registry list --status invalid", Reason: "inspect the offending plugins"},
	},
	"init-plugin": {
		{When: "after scaffold", Suggest: "dpkms registry submit ./<plugin-dir>", Reason: "publish the new plugin"},
	},
	"gen-docs": {
		{When: "after regen", Suggest: "git diff docs/", Reason: "review the freshly-generated docs"},
	},
}

func init() {
	rootCmd.AddCommand(devCmd)

	for _, sub := range []string{
		"validate-registry", "init-plugin", "gen-docs",
	} {
		name := sub
		c := &cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms dev %s", name),
			Long:       devLong[name],
			Deprecated: fmt.Sprintf("use 'dpkms dev %s' instead", name),
			RunE:       movedToDpkms("dev " + name),
			// Accept any args/flags so old invocations produce the deprecation
			// message instead of a usage error.
			DisableFlagParsing: true,
		}
		// All four dev verbs mutate local fs / build outputs.
		cliconv.WithSideEffect(c, cliconv.SideEffectWrite)
		cliconv.WithIdempotency(c, cliconv.IdempotencyNo)
		if ex, ok := devExamples[name]; ok {
			cliconv.WithExamples(c, ex)
		}
		if ns, ok := devNextSteps[name]; ok {
			cliconv.WithNextSteps(c, ns)
		}
		devCmd.AddCommand(c)
	}
}
