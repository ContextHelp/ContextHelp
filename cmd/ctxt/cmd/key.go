package cmd

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{
	Use:        "key",
	Short:      "[deprecated] moved to dpkms key",
	Long:       `Deprecated alias for the dpkms key command group. Forwards to 'dpkms key' subcommands.`,
	Deprecated: "use 'dpkms key' instead",
	RunE:       movedToDpkms("key"),
}

var keyLong = map[string]string{
	"init":   "Deprecated alias for 'dpkms key init'. Forwards invocations to dpkms.",
	"rotate": "Deprecated alias for 'dpkms key rotate'. Forwards invocations to dpkms.",
}

// keyExamples supplies 12fcc strict-gate Examples per subcommand. The
// surface is deprecated so the examples mirror the dpkms verb.
var keyExamples = map[string][]cliconv.Example{
	"init": {
		{Title: "Initialise the KMS", Command: "dpkms key init"},
		{Title: "Overwrite an existing keypair", Command: "dpkms key init --force"},
	},
	"rotate": {
		{Title: "Rotate the active signing key", Command: "dpkms key rotate"},
	},
}

// keyNextSteps supplies follow-ups for the key write verbs.
var keyNextSteps = map[string][]cliconv.NextStep{
	"init": {
		{When: "on success", Suggest: "dpkms secret set <name> <value>", Reason: "store the first secret under the newly initialised key"},
	},
	"rotate": {
		{When: "after rotate", Suggest: "dpkms config backup", Reason: "snapshot the new rotation_log.json before further changes"},
	},
}

func init() {
	rootCmd.AddCommand(keyCmd)

	for _, sub := range []string{"init", "rotate"} {
		name := sub
		c := &cobra.Command{
			Use:                name,
			Short:              fmt.Sprintf("[deprecated] moved to dpkms key %s", name),
			Long:               keyLong[name],
			Deprecated:         fmt.Sprintf("use 'dpkms key %s' instead", name),
			RunE:               movedToDpkms("key " + name),
			DisableFlagParsing: true,
		}
		// key init/rotate mutate KMS state; not idempotent.
		cliconv.WithSideEffect(c, cliconv.SideEffectWrite)
		cliconv.WithIdempotency(c, cliconv.IdempotencyNo)
		if ex, ok := keyExamples[name]; ok {
			cliconv.WithExamples(c, ex)
		}
		if ns, ok := keyNextSteps[name]; ok {
			cliconv.WithNextSteps(c, ns)
		}
		keyCmd.AddCommand(c)
	}
}
