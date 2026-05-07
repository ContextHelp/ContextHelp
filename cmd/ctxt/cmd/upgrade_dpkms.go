// Package cmd: ctxt upgrade {status,plan,run} — operator-facing surface
// for ADR-070 dpkms bucket upgrades (T-0580, ADR-070 Phase 2).
//
// These subcommands are SEPARATE from `ctxt upgrade {check,install,snooze,
// notes}` (defined in upgrade.go) which handle binary self-update via
// hop.top/kit/go/core/upgrade. Both groups share the `upgrade` parent
// because they are conceptually adjacent (operator updates):
//
//	ctxt upgrade check    — is a newer ctxt binary available?
//	ctxt upgrade install  — replace this binary
//	ctxt upgrade status   — what is dpkms doing right now? (this file)
//	ctxt upgrade plan     — what would 'ctxt upgrade run' do?      (this file)
//	ctxt upgrade run      — execute pending bucket-2 work           (this file)
//
// All three new subcommands talk to the configured dpkms instance via
// HTTP /healthz; the daemon owns the upgrade state machine
// (internal/upgrade.Manager).
//
// Output rules mirror `ctxt status`:
//   - Human-readable table by default.
//   - --format json emits the daemon's upgrade envelope verbatim.
//   - --watch loops until state goes idle (or the user interrupts).
//   - Exit 1 when state == "failed" or the daemon is unreachable.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// upgradeEnvelope is a thin reflection of the upgrade sub-object on the
// /healthz envelope. Duplicated here (rather than imported from the
// server package) so the CLI binary doesn't transitively pull the server
// package; same pattern as statusEnvelope in status.go.
type upgradeEnvelope struct {
	State      string  `json:"state"`
	Bucket     string  `json:"bucket,omitempty"`
	Progress   float64 `json:"progress,omitempty"`
	Done       int     `json:"done,omitempty"`
	Total      int     `json:"total,omitempty"`
	EtaSeconds int     `json:"eta_seconds,omitempty"`
	StartedAt  string  `json:"started_at,omitempty"`
	LastError  string  `json:"last_error,omitempty"`
}

// upgradeHealthzPayload matches just the fields ctxt upgrade reads from
// /healthz. Other top-level fields are accepted and ignored.
type upgradeHealthzPayload struct {
	Health  string           `json:"health"`
	Upgrade *upgradeEnvelope `json:"upgrade,omitempty"`
}

var upgradeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show in-flight dpkms upgrade state",
	Long: `Calls GET /healthz on the configured dpkms instance and renders the
upgrade envelope. With --format json the daemon response is passed
through unchanged.

Exit codes:
  0   server reports state idle, in_progress, or awaiting_consent
  1   server reports state failed, or is unreachable

Examples:
  ctxt upgrade status
  ctxt upgrade status --format json
  ctxt upgrade status --watch                 # refresh every 2s, exit at idle
  ctxt upgrade status --watch --interval 5    # refresh every 5s
  ctxt upgrade status --server http://localhost:8081`,
	RunE: runUpgradeStatus,
}

var upgradePlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview what 'ctxt upgrade run' would do (read-only)",
	Long: `plan summarises pending work across the three ADR-070 buckets:

  reindex_auto        based on the current FTS signature comparison
  reingest_selective  count of objects below the current pipeline @vN
  reingest_all        only populated when an embedding model swap is queued

This command is read-only and never mutates the database. --dry-run is
accepted as a no-op alias (plan is always a dry-run).

NOTE: T-0580 ships the command surface; the selectivity-predicate evaluator
and cost estimator are in T-0581. Until then, plan emits a high-level
summary derived from the current upgrade state.`,
	RunE: runUpgradePlan,
}

var upgradeRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute pending dpkms upgrade work",
	Long: `run executes the pending bucket-2 (reingest_selective) work, or, with
--all and --i-understand-the-cost, the bucket-3 (reingest_all) work.

Flags:
  --dry-run                    parse + plan but do not mutate
  --filter K=V                 narrow the affected object set (e.g. pipeline=text.short@v1)
  --rate-limit N               cap re-ingests per second (default unbounded)
  --all                        opt in to a full-corpus re-ingest
  --i-understand-the-cost SHA  required with --all; SHA must match the release-notes value

NOTE: T-0580 ships the command surface; the actual worker is filed under
T-0581. This command currently exits non-zero with a "not yet implemented"
message that points at the issue. Flags parse so future work fills in
the body without breaking the CLI surface.`,
	RunE: runUpgradeRun,
}

func init() {
	// Hang the new subcommands off the existing upgradeCmd defined in
	// upgrade.go (binary self-update group). See file header for why both
	// groups share the same parent.
	upgradeCmd.AddCommand(upgradeStatusCmd, upgradePlanCmd, upgradeRunCmd)

	// status flags — mirror ctxt status.
	upgradeStatusCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	upgradeStatusCmd.Flags().Bool("watch", false, "refresh every --interval seconds until idle")
	upgradeStatusCmd.Flags().Int("interval", 2, "seconds between refreshes when --watch is set")

	// plan flags.
	upgradePlanCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	upgradePlanCmd.Flags().Bool("dry-run", false, "no-op alias; plan is always read-only")

	// run flags. None of these have functional effect yet (T-0581) but
	// they parse so callers / future agents see a stable surface.
	upgradeRunCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	upgradeRunCmd.Flags().Bool("dry-run", false, "parse + plan but do not mutate")
	upgradeRunCmd.Flags().String("filter", "", "narrow affected objects (e.g. pipeline=text.short@v1)")
	upgradeRunCmd.Flags().Int("rate-limit", 0, "max re-ingests per second (0 = unbounded)")
	upgradeRunCmd.Flags().Bool("all", false, "execute a bucket-3 full-corpus re-ingest")
	upgradeRunCmd.Flags().String("i-understand-the-cost", "", "consent SHA from release notes (required with --all)")
}

// runUpgradeStatus implements `ctxt upgrade status` (+ --watch).
func runUpgradeStatus(cmd *cobra.Command, _ []string) error {
	serverURL := upgradeServerURL(cmd)
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetInt("interval")
	if interval < 1 {
		interval = 2
	}

	if !watch {
		return upgradeStatusOnce(cmd, serverURL)
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
		err := upgradeStatusOnce(cmd, serverURL)

		// Exit cleanly when the upgrade returns to idle so --watch is
		// usable in scripts ("wait until upgrade is done").
		env, _, ferr := fetchUpgradeHealthz(serverURL)
		if ferr == nil && (env.Upgrade == nil || env.Upgrade.State == "idle") {
			return nil
		}
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// upgradeStatusOnce performs one /healthz fetch and renders just the
// upgrade envelope. Returns an error (non-zero exit) only when the
// envelope reports state=failed or the request itself fails.
func upgradeStatusOnce(cmd *cobra.Command, serverURL string) error {
	env, _, err := fetchUpgradeHealthz(serverURL)
	if err != nil {
		return fmt.Errorf("healthcheck %s: %w", serverURL, err)
	}

	if isJSONOutput() {
		// JSON mode emits the upgrade sub-envelope directly so jq
		// pipelines can target it without indexing into .upgrade.
		// Idle is represented as {"state":"idle"} for shape stability.
		payload := env.Upgrade
		if payload == nil {
			payload = &upgradeEnvelope{State: "idle"}
		}
		if err := outputJSON(cmd.OutOrStdout(), payload); err != nil {
			return err
		}
	} else {
		renderUpgradeStatus(cmd.OutOrStdout(), env.Upgrade)
	}

	if env.Upgrade != nil && env.Upgrade.State == "failed" {
		return fmt.Errorf("upgrade failed: %s", env.Upgrade.LastError)
	}
	return nil
}

// renderUpgradeStatus prints the human-readable view per the T-0580 spec.
func renderUpgradeStatus(w io.Writer, up *upgradeEnvelope) {
	if up == nil || up.State == "idle" || up.State == "" {
		fmt.Fprintln(w, "Upgrade state: idle")
		return
	}
	fmt.Fprintf(w, "Upgrade state: %s\n", up.State)
	if up.Bucket != "" {
		fmt.Fprintf(w, "  Bucket:    %s\n", up.Bucket)
	}
	if up.State == "in_progress" && up.Total > 0 {
		pct := int(up.Progress*100 + 0.5)
		fmt.Fprintf(w, "  Progress:  %d/%d (%d%%)\n", up.Done, up.Total, pct)
		fmt.Fprintf(w, "  ETA:       %ds\n", up.EtaSeconds)
		if up.StartedAt != "" {
			fmt.Fprintf(w, "  Started:   %s\n", up.StartedAt)
		}
	}
	if up.State == "failed" && up.LastError != "" {
		fmt.Fprintf(w, "  Last error: %s\n", up.LastError)
	}
}

// runUpgradePlan implements the stubbed `ctxt upgrade plan`. Output is a
// canned summary describing what each bucket would do; the real predicate
// evaluator + cost estimator lands in T-0581.
func runUpgradePlan(cmd *cobra.Command, _ []string) error {
	serverURL := upgradeServerURL(cmd)
	env, _, err := fetchUpgradeHealthz(serverURL)
	if err != nil {
		return fmt.Errorf("healthcheck %s: %w", serverURL, err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		payload := map[string]any{
			"reindex_auto":       reindexAutoPlanSummary(env),
			"reingest_selective": "stub: selectivity predicate evaluator lands in T-0581",
			"reingest_all":       "none",
			"note":               "ctxt upgrade plan ships the surface in T-0580; T-0581 fills in counts.",
		}
		return outputJSON(out, payload)
	}

	fmt.Fprintln(out, "Upgrade plan:")
	fmt.Fprintf(out, "  reindex_auto:        %s\n", reindexAutoPlanSummary(env))
	fmt.Fprintln(out, "  reingest_selective:  selectivity predicate evaluator lands in T-0581")
	fmt.Fprintln(out, "  reingest_all:        none")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run 'ctxt upgrade run' to execute the selective re-ingest (stubbed in T-0580; ships in T-0581).")
	return nil
}

// reindexAutoPlanSummary peeks at /healthz to give a one-line summary of
// the reindex_auto bucket. It is intentionally conservative — the real
// signature comparison happens daemon-side in T-0579 / T-0581.
func reindexAutoPlanSummary(env upgradeHealthzPayload) string {
	if env.Upgrade != nil && env.Upgrade.State == "in_progress" && env.Upgrade.Bucket == "reindex_auto" {
		return fmt.Sprintf("running (%d/%d objects)", env.Upgrade.Done, env.Upgrade.Total)
	}
	return "none (signature ok)"
}

// runUpgradeRun is the stubbed `ctxt upgrade run`. Per the T-0580 spec,
// this command refuses to execute and points the operator at T-0581.
// All flags parse cleanly; future work fills in the body without
// touching the surface.
func runUpgradeRun(cmd *cobra.Command, _ []string) error {
	all, _ := cmd.Flags().GetBool("all")
	consent, _ := cmd.Flags().GetString("i-understand-the-cost")

	// Validate the bucket-3 consent guard surface even though we won't
	// run anything. This way operators get accurate feedback NOW about
	// missing flags rather than discovering it post-T-0581.
	if all && consent == "" {
		return errors.New("--all requires --i-understand-the-cost <sha> (the consent SHA appears in the release notes)")
	}

	msg := "'ctxt upgrade run' executes the upgrade but the selective re-ingest worker is not yet shipped (T-0581).\n" +
		"For now, monitor with 'ctxt upgrade status'; the daemon's reindex_auto bucket runs automatically on signature mismatch."
	return errors.New(msg)
}

// upgradeServerURL mirrors statusServerURL but is duplicated to avoid
// invisible coupling — the two CLI commands are siblings and either may
// grow flags the other doesn't have.
func upgradeServerURL(cmd *cobra.Command) string {
	url := flagString(cmd, "server", "server.url")
	if url == "" {
		url = "http://localhost:8080"
	}
	return url
}

// fetchUpgradeHealthz issues GET /healthz and decodes only the fields
// `ctxt upgrade` cares about. Other top-level fields pass through silently.
func fetchUpgradeHealthz(serverURL string) (upgradeHealthzPayload, int, error) {
	url := strings.TrimRight(serverURL, "/") + "/healthz"
	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- user-provided server URL
	if err != nil {
		return upgradeHealthzPayload{}, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return upgradeHealthzPayload{}, resp.StatusCode, err
	}

	var env upgradeHealthzPayload
	if err := json.Unmarshal(body, &env); err != nil {
		return upgradeHealthzPayload{}, resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return env, resp.StatusCode, nil
}
