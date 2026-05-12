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
		secretCmd.AddCommand(c)
	}
}
