// Package cmd hosts the ctxt CLI. lateral.go adds the `ctxt lateral`
// subcommand tree, the daemon entrypoint for the lateral discovery
// substrate.
//
// Subcommands:
//
//	ctxt lateral start         long-running daemon process
//	ctxt lateral status        not yet wired (returns daemon.ErrNotWired)
//	ctxt lateral config show   render the effective lateral.* config block
//
// The default action is `start` so `ctxt lateral` alone runs the daemon
// in the foreground.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	adapterbus "github.com/ideacrafterslabs/ctxt/internal/lateral/adapters/bus"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
)

// errLateralNotWired re-exports daemon.ErrNotWired so subcommand stubs
// surface the same sentinel CLI consumers grep on.
var errLateralNotWired = daemon.ErrNotWired

var lateralCmd = &cobra.Command{
	Use:   "lateral",
	Short: "Lateral discovery substrate (daemon)",
	Long: `lateral runs the lateral discovery substrate as a long-running
daemon. It subscribes to capture-pipeline events, dispatches strategies
through the registry, and emits candidates downstream.

The substrate is opt-in: each strategy is gated by config. Operators
turn families on via the strategies.<name>.enabled keys in the layered
config (system/user/project). See docs/ctxt/cli-lateral.md for the full
reference.

When invoked with no subcommand, ctxt lateral defaults to 'start'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLateralStart(cmd, args)
	},
}

var lateralStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the lateral discovery daemon",
	Long: `start launches the lateral substrate in the foreground:
loads config, builds the strategy registry, wires kit collaborators
(bus, breaker, job engine), subscribes to capture events, and drives
the cold-cycle reaper. Stops on SIGINT / SIGTERM.

Note: in T-0321 v1, the daemon ships with stub adapters wired only
when the corresponding strategy is gated on. Production deployments
also wire real LLM, GitHub, and platform clients via the lateral
config; the placeholder daemon here is sufficient for the smoke
test (T-0322) and serves as the integration surface tracked in PR
#TBD.`,
	RunE: runLateralStart,
}

var lateralStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Query a running lateral daemon",
	Long: `status reports on a running daemon: registered strategies,
breaker state, recent reaper-cycle stats. Reads via the bus; the
daemon must be running. Currently returns daemon.ErrNotWired —
status reporting is part of the operational slice (P5 follow-on).`,
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
	// `ctxt lateral` (no subcommand) defaults to `lateral start` — a
	// long-running foreground daemon. Mark the depth-1 leaf interactive
	// so the validator accepts it; deeper subcommands (start/status/
	// config show) are owned by a sibling subtree.
	cliconv.WithSideEffect(lateralCmd, cliconv.SideEffectInteractive)
	cliconv.WithExamples(lateralCmd, []cliconv.Example{
		{Title: "Run the daemon in the foreground", Command: "ctxt lateral"},
		{Title: "Run with an extra config layer", Command: "ctxt lateral --lateral-config ./lateral.yaml"},
	})
	cliconv.WithNextSteps(lateralCmd, []cliconv.NextStep{
		{When: "to inspect the resolved config", Suggest: "ctxt lateral config show", Reason: "verify which strategies are gated on"},
		{When: "to query a running daemon", Suggest: "ctxt lateral status", Reason: "see registered strategies and breaker state"},
	})
	// "lateral" is not in kit's defaultIdempotency table; the daemon
	// holds open subscriptions and a poller loop, which is not a
	// replay-safe operation.
	cliconv.WithIdempotency(lateralCmd, cliconv.IdempotencyNo)

	// start: long-running foreground daemon; not replay-safe.
	cliconv.WithSideEffect(lateralStartCmd, cliconv.SideEffectInteractive)
	cliconv.WithIdempotency(lateralStartCmd, cliconv.IdempotencyNo)
	// status: read-only query against a running daemon.
	cliconv.WithSideEffect(lateralStatusCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(lateralStatusCmd, cliconv.IdempotencyYes)
	// config: intermediate group; mark hierarchical so the shape
	// validator accepts the depth-3 leaf below.
	cliconv.MarkHierarchical(lateralConfigCmd)
	// config show: read-only config resolution.
	cliconv.WithSideEffect(lateralConfigShowCmd, cliconv.SideEffectRead)

	lateralCmd.AddCommand(lateralStartCmd)
	lateralCmd.AddCommand(lateralStatusCmd)
	lateralCmd.AddCommand(lateralConfigCmd)
	lateralConfigCmd.AddCommand(lateralConfigShowCmd)

	// Stable flag surface; documented by T-0323.
	for _, c := range []*cobra.Command{lateralStartCmd, lateralConfigShowCmd} {
		c.Flags().String("lateral-config", "", "extra config file appended as the highest-priority layer")
	}
	lateralStartCmd.Flags().String("worker-id", "", "worker identifier for the cold-cycle poller (default: lateral-daemon-default)")
	lateralStartCmd.Flags().Duration("poll-interval", 0, "cold-cycle poll interval (default: 5s)")
}

// loadOptionsFromCmd extracts the daemon LoadOptions from cobra
// flags. The single --lateral-config flag is appended as an extra
// layer (highest priority). System/User/Project paths come from
// the kit/cli-managed XDG resolution; for now we leave them empty
// and let extras + defaults drive behaviour.
func loadOptionsFromCmd(cmd *cobra.Command) daemon.LoadOptions {
	opts := daemon.LoadOptions{
		EnvPrefix: "CTXT_LATERAL",
	}
	if extra, _ := cmd.Flags().GetString("lateral-config"); extra != "" {
		opts.ExtraConfigPaths = []string{extra}
	}
	return opts
}

// runLateralStart is the daemon entrypoint. Loads config, builds an
// in-memory bus + registry (no adapters wired in v1 — they're plumbed
// per-strategy by operators), starts the lifecycle layer, and runs
// the cold-cycle poller until SIGINT/SIGTERM.
//
// Production wiring (LLM, GitHub, platforms) is delegated to operator
// configuration: each strategy panics loud at boot if its required
// deps aren't supplied. v1 ships with all strategies disabled by
// default so a fresh `ctxt lateral start` returns immediately on the
// poller loop without doing anything dangerous.
func runLateralStart(cmd *cobra.Command, _ []string) error {
	cfg, err := daemon.LoadConfig(loadOptionsFromCmd(cmd))
	if err != nil {
		return fmt.Errorf("lateral start: load config: %w", err)
	}

	// In-memory bus is the v1 default. Operators wiring kit's
	// network adapter, sqlite adapter, etc. extend cmd/ctxt/cmd/
	// lateral_wiring.go (P5 follow-on).
	b := bus.New(bus.WithEnforce(bus.ModeOff))
	defer func() { _ = b.Close(context.Background()) }()
	pub := adapterbus.New(b)

	// Build the registry. v1 default config = all strategies disabled
	// at registration time (jit cfg.Enabled=false; github default-on
	// requires an APIClient operators haven't wired); the resulting
	// registry is empty until adapters land.
	deps := daemon.Deps{Publisher: pub}
	if cfg.GitHub.EnableParent {
		// Operators must provide an APIClient via a custom build —
		// v1 daemon doesn't inject one. Fall back to disabling github
		// rather than panicking.
		cfg.GitHub.EnableParent = false
		cfg.GitHub.EnableGist = false
		cfg.GitHub.EnableSecurityAdvisory = false
		fmt.Fprintln(cmd.ErrOrStderr(),
			"lateral: github strategies disabled — APIClient wiring is a v1 follow-on")
	}
	reg, snap := daemon.Build(cfg, deps)
	fmt.Fprintf(cmd.OutOrStdout(),
		"lateral: registered strategies — github=%d roster=%d jit=%v\n",
		snap.GitHub, snap.Roster, snap.JIT)

	worker, _ := cmd.Flags().GetString("worker-id")
	pollIntv, _ := cmd.Flags().GetDuration("poll-interval")
	lc, err := daemon.NewLifecycle(daemon.LifecycleOptions{
		Bus:          b,
		Registry:     reg,
		Publisher:    pub,
		WorkerID:     worker,
		PollInterval: pollIntv,
	})
	if err != nil {
		return fmt.Errorf("lateral start: lifecycle: %w", err)
	}

	// Apply per-strategy gate + sampler from initial config (T-0328 +
	// T-0329). Operators flip strategies live by editing the YAML
	// and sending SIGHUP; the goroutine below re-reads + re-applies
	// on each signal.
	lc.ApplyGate(cfg)
	lc.ApplySampler(cfg)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// SIGHUP → reload + re-apply gate. Keeps the daemon's
	// per-strategy enable / kill-switch flags responsive.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				newCfg, lerr := daemon.LoadConfig(loadOptionsFromCmd(cmd))
				if lerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "lateral: SIGHUP reload failed: %v\n", lerr)
					continue
				}
				lc.ApplyGate(newCfg)
				lc.ApplySampler(newCfg)
				fmt.Fprintln(cmd.OutOrStdout(), "lateral: SIGHUP reload applied")
			}
		}
	}()

	if err := lc.Start(ctx); err != nil {
		return fmt.Errorf("lateral start: subscribe: %w", err)
	}
	defer lc.Stop()

	fmt.Fprintln(cmd.OutOrStdout(), "lateral: daemon running — Ctrl-C to stop")
	if err := lc.Run(ctx); err != nil && err != context.Canceled {
		return fmt.Errorf("lateral start: poller: %w", err)
	}
	return nil
}

// runLateralStatus stays a stub — operational reads are P5 work.
func runLateralStatus(_ *cobra.Command, _ []string) error {
	return errLateralNotWired
}

// runLateralConfigShow renders the merged config block as JSON for
// operator triage. Uses the same loader the daemon uses, so what you
// see is what `lateral start` would consume.
func runLateralConfigShow(cmd *cobra.Command, _ []string) error {
	cfg, err := daemon.LoadConfig(loadOptionsFromCmd(cmd))
	if err != nil {
		return fmt.Errorf("lateral config show: %w", err)
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("lateral config show: encode: %w", err)
	}
	return nil
}

