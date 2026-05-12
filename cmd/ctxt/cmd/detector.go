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
		detectorCmd.AddCommand(c)
	}
}
