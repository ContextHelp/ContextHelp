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

var shutdownCmd = &cobra.Command{
	Use:     "shutdown",
	Aliases: []string{"stop"},
	Short:   "Stop a running dpkms instance (graceful)",
	Long: `Send SIGTERM to a running dpkms instance to trigger a graceful shutdown.

The target instance drains in-flight jobs, closes all servers, and exits.

By default targets the instance on --server-url port. Use --port to target
a specific instance when multiple are running.

Examples:
  dpkms shutdown
  dpkms stop
  dpkms shutdown --port 8081`,
	RunE: runShutdown,
}

func init() {
	rootCmd.AddCommand(shutdownCmd)
	shutdownCmd.Flags().Int("port", 0, "port of the instance to shut down (default: server-url port)")

	// SIGTERM to a live daemon. The instance drains and exits; in-flight
	// work is interrupted and the process does not come back on its own.
	// Destructive.
	cliconv.WithSideEffect(shutdownCmd, cliconv.SideEffectDestructive)
}

func runShutdown(cmd *cobra.Command, _ []string) error {
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
			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("signal pid %d: %w", info.PID, err)
			}
			fmt.Printf("Sent SIGTERM to dpkms (pid %d, port %d)\n", info.PID, port)
			return nil
		}
	}

	return fmt.Errorf("no running dpkms instance found on port %d", port)
}
