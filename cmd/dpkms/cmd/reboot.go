package cmd

import (
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

var rebootCmd = &cobra.Command{
	Use:   "reboot",
	Short: "Restart a running dpkms instance (graceful)",
	Long: `Send SIGHUP to a running dpkms instance to trigger a graceful restart.

The target instance drains in-flight jobs, shuts down, then the caller
is responsible for re-launching dpkms serve (or use a process supervisor).

By default targets the instance on --server-url port. Use --port to target
a specific instance when multiple are running.

Examples:
  dpkms reboot
  dpkms reboot --port 8081`,
	RunE: runReboot,
}

func init() {
	rootCmd.AddCommand(rebootCmd)
	rebootCmd.Flags().Int("port", 0, "port of the instance to reboot (default: server-url port)")

	// SIGHUP to a live daemon: drains and shuts down, leaving relaunch to
	// the caller or a supervisor. Same interruption profile as shutdown.
	// Destructive.
	cliconv.WithSideEffect(rebootCmd, cliconv.SideEffectDestructive)
}

func runReboot(cmd *cobra.Command, _ []string) error {
	port, _ := cmd.Flags().GetInt("port")
	if port == 0 {
		port = viper.GetInt("server.port")
	}

	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}

	instances, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	for _, info := range instances {
		if info.Port == port {
			proc, err := os.FindProcess(info.PID)
			if err != nil {
				return fmt.Errorf("find process %d: %w", info.PID, err)
			}
			if err := proc.Signal(syscall.SIGHUP); err != nil {
				return fmt.Errorf("signal pid %d: %w", info.PID, err)
			}
			fmt.Printf("Sent SIGHUP to dpkms (pid %d, port %d)\n", info.PID, port)
			return nil
		}
	}

	return fmt.Errorf("no running dpkms instance found on port %d", port)
}
