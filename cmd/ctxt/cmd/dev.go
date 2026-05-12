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
	"reindex-vectors":   "Deprecated alias for 'dpkms dev reindex-vectors'. Forwards invocations to dpkms.",
	"validate-registry": "Deprecated alias for 'dpkms dev validate-registry'. Forwards invocations to dpkms.",
	"init-plugin":       "Deprecated alias for 'dpkms dev init-plugin'. Forwards invocations to dpkms.",
	"gen-docs":          "Deprecated alias for 'dpkms dev gen-docs'. Forwards invocations to dpkms.",
}

func init() {
	rootCmd.AddCommand(devCmd)

	for _, sub := range []string{
		"reindex-vectors", "validate-registry", "init-plugin", "gen-docs",
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
		devCmd.AddCommand(c)
	}
}
