package cmd

import (
	"syscall"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
)

var rebootCmd = &cobra.Command{
	Use:   "reboot",
	Short: "Restart a running dpkms instance (graceful)",
	Long: `Send SIGHUP to a running dpkms instance to trigger a graceful restart.

The target instance drains in-flight jobs, shuts down, then the caller
is responsible for re-launching dpkms serve (or use a process supervisor).

By default targets the instance listening on server.port (config file,
overridden by CH_SERVER_PORT). Use --port to target a specific instance
when multiple are running.

Examples:
  dpkms reboot
  dpkms reboot --port 8081`,
	RunE: runReboot,
}

func init() {
	rootCmd.AddCommand(rebootCmd)
	rebootCmd.Flags().Int("port", 0, "port of the instance to reboot (default: server.port / CH_SERVER_PORT)")

	// SIGHUP to a live daemon: drains and shuts down, leaving relaunch to
	// the caller or a supervisor. Same interruption profile as shutdown.
	// Destructive.
	cliconv.WithSideEffect(rebootCmd, cliconv.SideEffectDestructive)
}

func runReboot(cmd *cobra.Command, _ []string) error {
	return signalInstance(cmd, syscall.SIGHUP, "SIGHUP")
}
