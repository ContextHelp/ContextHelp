package cmd

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:        "secret",
	Short:      "[deprecated] moved to dpkms secret",
	Long:       `Deprecated alias for the dpkms secret command group. Forwards to 'dpkms secret' subcommands.`,
	Deprecated: "use 'dpkms secret' instead",
	RunE:       movedToDpkms("secret"),
}

var secretSideEffect = map[string]cliconv.SideEffect{
	"get":  cliconv.SideEffectRead,
	"set":  cliconv.SideEffectWrite,
	"list": cliconv.SideEffectRead,
}

var secretIdempotency = map[string]cliconv.Idempotency{
	"get":  cliconv.IdempotencyYes,
	"set":  cliconv.IdempotencyYes,
	"list": cliconv.IdempotencyYes,
}

var secretLong = map[string]string{
	"get":  "Deprecated alias for 'dpkms secret get'. Forwards invocations to dpkms.",
	"set":  "Deprecated alias for 'dpkms secret set'. Forwards invocations to dpkms.",
	"list": "Deprecated alias for 'dpkms secret list'. Forwards invocations to dpkms.",
}

// secretExamples supplies 12fcc strict-gate Examples per subcommand;
// the surface is deprecated so the examples mirror the dpkms verb.
var secretExamples = map[string][]cliconv.Example{
	"get": {
		{Title: "Read a secret", Command: "dpkms secret get my-secret"},
		{Title: "Read as JSON", Command: "dpkms secret get my-secret --format json"},
	},
	"set": {
		{Title: "Set a secret", Command: "dpkms secret set my-secret s3cret-value"},
		{Title: "Read value from stdin", Command: "echo s3cret-value | dpkms secret set my-secret --stdin"},
	},
	"list": {
		{Title: "List all secrets", Command: "dpkms secret list"},
		{Title: "Filter by prefix", Command: "dpkms secret list --prefix github."},
	},
}

// secretNextSteps supplies follow-ups for secret writes.
var secretNextSteps = map[string][]cliconv.NextStep{
	"set": {
		{When: "on success", Suggest: "dpkms secret get <name>", Reason: "verify the secret round-trips correctly"},
	},
}

func init() {
	rootCmd.AddCommand(secretCmd)

	for _, sub := range []string{"get", "set", "list"} {
		name := sub
		c := &cobra.Command{
			Use:                name,
			Short:              fmt.Sprintf("[deprecated] moved to dpkms secret %s", name),
			Long:               secretLong[name],
			Deprecated:         fmt.Sprintf("use 'dpkms secret %s' instead", name),
			RunE:               movedToDpkms("secret " + name),
			DisableFlagParsing: true,
		}
		cliconv.WithSideEffect(c, secretSideEffect[name])
		cliconv.WithIdempotency(c, secretIdempotency[name])
		if ex, ok := secretExamples[name]; ok {
			cliconv.WithExamples(c, ex)
		}
		if ns, ok := secretNextSteps[name]; ok {
			cliconv.WithNextSteps(c, ns)
		}
		secretCmd.AddCommand(c)
	}
}
