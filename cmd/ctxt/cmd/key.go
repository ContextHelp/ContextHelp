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
		keyCmd.AddCommand(c)
	}
}
