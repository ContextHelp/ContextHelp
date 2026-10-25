package cmd

import (
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// Commands that work on the local store. --server is a global flag and
// names a remote dpkms these commands never reach, so it is refused
// instead of being ignored while the local store changes.
func init() {
	refuseServerFlag(
		upgradePlanCmd, upgradeRunCmd,
		registryAddCmd, registryRemoveCmd, registrySyncCmd, registryApproveCmd, registryRejectCmd,
		remindSetCmd, remindClearCmd,
		resurfaceRefreshCmd, resurfaceDismissCmd,
		embeddingsRegisterCmd, embeddingsSetDefaultCmd, embeddingsDeprecateCmd, embeddingsPurgeCmd, embeddingsMigrateCmd,
		ingestCmd,
	)
}

// refuseServerFlag makes --server a usage error (exit 2) on each of
// cmds, checked before any other pre-run step.
func refuseServerFlag(cmds ...*cobra.Command) {
	for _, c := range cmds {
		next := c.PreRunE
		c.PreRunE = func(cmd *cobra.Command, args []string) error {
			if f := cmd.Flag("server"); f != nil && f.Changed {
				e := output.UsageError(cmd.CommandPath() + " works on the local store; --server does not apply")
				e.SuggestedFix = "drop --server, or run the command on the dpkms host"
				return e
			}
			if next != nil {
				return next(cmd, args)
			}
			return nil
		}
	}
}
