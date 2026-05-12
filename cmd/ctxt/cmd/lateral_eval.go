// lateral_eval.go adds the `ctxt lateral eval` subcommand tree —
// offline replay + metrics for the lateral discovery substrate.
//
// Subcommands:
//
//	ctxt lateral eval replay <fixtures.jsonl>   dispatch fixtures, dump JSON report
//	ctxt lateral eval metrics <fixtures.jsonl>  same as replay + per-strategy precision/recall (T-0326)
//
// Both commands honor the same --lateral-config flag the daemon
// commands use; the replay shares the daemon's registry construction
// (daemon.Build) so eval-time strategy selection mirrors production.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	lateral "github.com/ideacrafterslabs/ctxt/internal/lateral"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/daemon"
	"github.com/ideacrafterslabs/ctxt/internal/lateral/eval"
)

var lateralEvalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Offline evaluation of the lateral discovery substrate",
	Long: `eval replays JSONL fixtures of CapturedEvents through a
configured registry, captures emitted candidates, and reports the
result. Used for ad-hoc QA + by the lateral-eval CI workflow to gate
PR merges on regression.

The fixture format is line-delimited JSON; each line is a Fixture
object with "event" and "expect" keys. See internal/lateral/eval for
the schema.`,
}

var lateralEvalReplayCmd = &cobra.Command{
	Use:   "replay <fixtures.jsonl>",
	Short: "Replay a fixture file through the registry, print a JSON report",
	Long: `replay parses a JSONL fixture file (one Fixture per line), dispatches
each event through the configured strategy registry, captures the emitted
candidates, and prints a JSON report mapping fixture → candidates. Used
by the lateral-eval CI workflow to gate PR merges; also useful for ad-hoc
QA after touching a strategy's Probe/Applies surface.`,
	Args: cobra.ExactArgs(1),
	RunE: runLateralEvalReplay,
}

var lateralEvalMetricsCmd = &cobra.Command{
	Use:   "metrics <fixtures.jsonl>",
	Short: "Replay + print per-strategy precision/recall/negative-pass table",
	Long: `metrics runs the same fixture replay as 'replay' and then aggregates
per-strategy precision, recall, and negative-pass rate against the expected
candidates declared in each Fixture. Default output is a text table; pass
--json to emit the same numbers as machine-readable JSON.`,
	Args: cobra.ExactArgs(1),
	RunE: runLateralEvalMetrics,
}

func init() {
	lateralCmd.AddCommand(lateralEvalCmd)
	lateralEvalCmd.AddCommand(lateralEvalReplayCmd)
	lateralEvalCmd.AddCommand(lateralEvalMetricsCmd)

	// eval is an intermediate group with depth-3 leaves below.
	cliconv.MarkHierarchical(lateralEvalCmd)
	// replay + metrics are read-only fixture replays.
	cliconv.WithSideEffect(lateralEvalReplayCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(lateralEvalReplayCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(lateralEvalReplayCmd, []cliconv.Example{
		{Title: "Replay a fixture file", Command: "ctxt lateral eval replay ./fixtures.jsonl"},
		{Title: "Replay with extra config", Command: "ctxt lateral eval replay ./fixtures.jsonl --lateral-config ./lateral.local.yaml"},
	})
	cliconv.WithSideEffect(lateralEvalMetricsCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(lateralEvalMetricsCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(lateralEvalMetricsCmd, []cliconv.Example{
		{Title: "Compute per-strategy metrics", Command: "ctxt lateral eval metrics ./fixtures.jsonl"},
		{Title: "Emit metrics as JSON", Command: "ctxt lateral eval metrics ./fixtures.jsonl --json"},
	})

	for _, c := range []*cobra.Command{lateralEvalReplayCmd, lateralEvalMetricsCmd} {
		c.Flags().String("lateral-config", "", "extra config file appended as the highest-priority layer")
	}
	lateralEvalMetricsCmd.Flags().Bool("json", false, "emit JSON instead of the default text table")
}

// buildEvalRegistry constructs a registry suitable for offline replay.
// Strategies that need real LLM/API/fetcher clients are skipped — the
// eval surface focuses on URL-shape regression (Applies + structural
// Probe). Wiring a richer client set is an operator extension via a
// future --eval-deps file (out of scope for the v1 surface).
func buildEvalRegistry(cmd *cobra.Command) (*lateral.Registry, error) {
	cfg, err := daemon.LoadConfig(loadOptionsFromCmd(cmd))
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	// Force JIT off and GitHub off — these need adapters the eval
	// surface doesn't construct. Roster strategies are URL-structural
	// for the no-client case (they degrade to URL-only candidates).
	cfg.JIT.Enabled = false
	cfg.GitHub.EnableParent = false
	cfg.GitHub.EnableGist = false
	cfg.GitHub.EnableSecurityAdvisory = false

	reg, _ := daemon.Build(cfg, daemon.Deps{})
	return reg, nil
}

func runLateralEvalReplay(cmd *cobra.Command, args []string) error {
	reg, err := buildEvalRegistry(cmd)
	if err != nil {
		return fmt.Errorf("lateral eval replay: %w", err)
	}
	report, err := eval.Replay(context.Background(), reg, args[0])
	if err != nil {
		return fmt.Errorf("lateral eval replay: %w", err)
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("lateral eval replay: encode: %w", err)
	}
	return nil
}

func runLateralEvalMetrics(cmd *cobra.Command, args []string) error {
	reg, err := buildEvalRegistry(cmd)
	if err != nil {
		return fmt.Errorf("lateral eval metrics: %w", err)
	}
	report, err := eval.Replay(context.Background(), reg, args[0])
	if err != nil {
		return fmt.Errorf("lateral eval metrics: %w", err)
	}
	metrics := eval.ComputeMetrics(report)
	jsonOut, _ := cmd.Flags().GetBool("json")
	if jsonOut {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(metrics); err != nil {
			return fmt.Errorf("lateral eval metrics: encode: %w", err)
		}
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), eval.RenderMetricsTable(metrics))
	return nil
}
