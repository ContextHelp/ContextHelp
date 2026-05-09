// Package cmd hosts the ctxt CLI. lateral.go adds the `ctxt lateral`
// subcommand tree, the daemon entrypoint for the lateral discovery
// substrate. T-0312 ships only the cobra scaffolding; subsequent tasks
// (T-0313..T-0321) fill in config loading, adapters, registry wiring,
// and lifecycle.
//
// Subcommands:
//
//	ctxt lateral start         long-running daemon process
//	ctxt lateral status        operational query (registry + breaker state)
//	ctxt lateral config show   render the effective lateral.* config block
//
// The default action is `start` so `ctxt lateral` alone runs the daemon
// in the foreground. All three subcommands return a clear "not yet
// wired" error in T-0312; later tasks replace those stubs.
package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
	"github.com/spf13/cobra"
)

// errLateralNotWired re-exports daemon.ErrNotWired so subcommand stubs
// surface the same sentinel CLI consumers grep on. Stable across the
// T-0312..T-0321 wiring window.
var errLateralNotWired = daemon.ErrNotWired

var lateralCmd = &cobra.Command{
	Use:   "lateral",
	Short: "Lateral discovery substrate (daemon)",
	Long: `lateral runs the lateral discovery substrate as a long-running
daemon. It subscribes to capture-pipeline events, dispatches strategies
through the registry, and emits candidates downstream.

The substrate is opt-in: every strategy ships with enabled=false in the
default config. Operators turn families on via lateral.strategies.<name>.
enabled in the layered config (system/user/project). See
docs/ctxt/cli-lateral.md for the full reference.

When invoked with no subcommand, ctxt lateral defaults to 'start'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Default action: run the daemon. Mirrors how `ctxt` (root)
		// dispatches to analyze without an explicit subcommand.
		return runLateralStart(cmd, args)
	},
}

var lateralStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the lateral discovery daemon",
	Long: `start launches the lateral substrate in the foreground:
loads config, builds the strategy registry, wires kit collaborators
(bus, breaker, job engine), subscribes to capture events, and drives
the cold-cycle reaper. Stops on SIGINT / SIGTERM; SIGHUP triggers a
config reload.`,
	RunE: runLateralStart,
}

var lateralStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Query a running lateral daemon",
	Long: `status reports on a running daemon: registered strategies,
breaker state, recent reaper-cycle stats. Reads via the bus; the
daemon must be running.`,
	RunE: runLateralStatus,
}

var lateralConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect lateral configuration",
}

var lateralConfigShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective lateral.* config block",
	Long: `show resolves the layered config (system/user/project + extras)
and prints the merged result. Useful for debugging gate decisions
("why isn't strategy X firing?").`,
	RunE: runLateralConfigShow,
}

func init() {
	rootCmd.AddCommand(lateralCmd)
	lateralCmd.AddCommand(lateralStartCmd)
	lateralCmd.AddCommand(lateralStatusCmd)
	lateralCmd.AddCommand(lateralConfigCmd)
	lateralConfigCmd.AddCommand(lateralConfigShowCmd)

	// Flags for `lateral start`. Each later task layers in real usage;
	// T-0312 ships the surface so docs (T-0323) can reference stable
	// flag names without churning the contract.
	lateralStartCmd.Flags().String("lateral-config", "", "extra config file appended as the highest-priority layer")
	lateralStartCmd.Flags().String("worker-id", "", "worker identifier for the cold-cycle poller (default: hostname)")
	lateralStartCmd.Flags().Duration("poll-interval", 0, "cold-cycle poll interval (default: 5s)")
}

// runLateralStart is the daemon entrypoint stub. T-0313..T-0321 replace
// the body with: load config, build registry, wire deps, drive the
// poller until ctx is cancelled.
func runLateralStart(_ *cobra.Command, _ []string) error {
	return errLateralNotWired
}

// runLateralStatus is the operational-query stub. T-0322's smoke test
// validates the daemon-side state machine; status reporting reads via
// the bus and is part of the lifecycle slice.
func runLateralStatus(_ *cobra.Command, _ []string) error {
	return errLateralNotWired
}

// runLateralConfigShow is the config-inspection stub. T-0313 lands the
// real implementation as part of config loading.
func runLateralConfigShow(_ *cobra.Command, _ []string) error {
	return errLateralNotWired
}
