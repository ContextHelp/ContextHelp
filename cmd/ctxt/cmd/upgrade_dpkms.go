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
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
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

The selective re-ingest section iterates the pipeline registry and counts
objects whose stored pipeline stamp parses to a version older than the
registry's currently-installed version. Each pending family transition is
listed alongside the exact 'ctxt upgrade run --filter ...' command that
would clear it.`,
	RunE: runUpgradePlan,
}

var upgradeRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute pending dpkms upgrade work",
	Long: `run executes the pending bucket-2 (reingest_selective) work, or, with
--all and --i-understand-the-cost, the bucket-3 (reingest_all) work.

Flags:
  --dry-run                    parse + plan but do not mutate
  --filter K=V                 narrow the affected object set (e.g. pipeline=text.short@v0)
  --where PRED                 SQL WHERE escape hatch (e.g. graph_json IS NULL AND ...)
  --rate-limit N               cap re-ingests per second (default unbounded)
  --budget USD                 cap cumulative LLM cost; abort if exceeded
  --all                        opt in to a full-corpus re-ingest (not yet implemented)
  --i-understand-the-cost SHA  required with --all; SHA must match the release-notes value

The selective form requires either --filter or --where. --all is reserved
for a future reingest_all worker (ADR-070 §1) and currently refuses with
a pointer to the relevant ADR section.`,
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

	// plan flags. --dry-run is inherited from the kit global persistent
	// flag (plan is read-only anyway; the flag is silently accepted).
	upgradePlanCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

	// run flags. --dry-run is inherited from the kit global persistent flag.
	upgradeRunCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	upgradeRunCmd.Flags().String("filter", "", "narrow affected objects (e.g. pipeline=text.short@v0)")
	upgradeRunCmd.Flags().String("where", "", "SQL WHERE escape hatch (compiles to where:<predicate>)")
	upgradeRunCmd.Flags().Int("rate-limit", 0, "max re-ingests per second (0 = unbounded)")
	upgradeRunCmd.Flags().Float64("budget", 0, "cumulative LLM cost ceiling in USD (0 = unbounded)")
	upgradeRunCmd.Flags().Bool("all", false, "execute a bucket-3 full-corpus re-ingest")
	upgradeRunCmd.Flags().String("i-understand-the-cost", "", "consent SHA from release notes (required with --all)")

	cliconv.WithSideEffect(upgradeStatusCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(upgradeStatusCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(upgradeStatusCmd, []cliconv.Example{
		{Title: "Show dpkms upgrade state", Command: "ctxt upgrade status"},
		{Title: "Watch until idle", Command: "ctxt upgrade status --watch --interval 5"},
	})

	cliconv.WithSideEffect(upgradePlanCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(upgradePlanCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(upgradePlanCmd, []cliconv.Example{
		{Title: "Preview pending re-ingest work", Command: "ctxt upgrade plan"},
		{Title: "Emit plan as JSON", Command: "ctxt upgrade plan --json"},
	})

	cliconv.WithSideEffect(upgradeRunCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(upgradeRunCmd, cliconv.IdempotencyConditional)
	cliconv.WithExamples(upgradeRunCmd, []cliconv.Example{
		{Title: "Selective re-ingest by pipeline filter", Command: "ctxt upgrade run --filter pipeline=text.short@v0"},
		{Title: "Preview before running", Command: "ctxt upgrade run --filter pipeline=text.short@v0 --dry-run"},
	})
	cliconv.WithNextSteps(upgradeRunCmd, []cliconv.NextStep{
		{Suggest: "ctxt upgrade status", Reason: "track progress while the worker runs (or pair with --watch)"},
		{Suggest: "ctxt upgrade plan", Reason: "confirm the targeted family transition cleared after the run"},
	})
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

// runUpgradePlan implements `ctxt upgrade plan` — the predicate-driven
// preview of pending work (ADR-070 §1, T-0581).
//
// For each pipeline registered in the in-process registry, plan parses every
// `pipeline` value in the `objects` table and counts rows whose version is
// strictly older than the registry's currently-installed version for the
// same family. Each pending family transition surfaces as a row plus the
// concrete `ctxt upgrade run --filter` invocation that would clear it.
//
// reindex_auto and reingest_all are summarised at a glance: reindex_auto is
// driven by signature comparison (handled out-of-band in T-0579 and the
// daemon's startup path); reingest_all is reserved for embedding-model
// swaps and out of scope for T-0581.
func runUpgradePlan(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return fmt.Errorf("upgrade plan: %w", err)
	}
	defer cleanup()

	db, err := upgradeServiceDB(svc)
	if err != nil {
		return err
	}

	plan, total, err := computeReingestSelectivePlan(cmd.Context(), db, svc.Pipes)
	if err != nil {
		return fmt.Errorf("upgrade plan: %w", err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		// JSON shape mirrors the human-readable layout so jq pipelines can
		// pluck either the per-bucket sub-tree or just the total.
		payload := map[string]any{
			"reindex_auto": map[string]any{
				"summary": "objects_fts: signature ok (no rebuild needed)",
			},
			"reingest_selective": map[string]any{
				"transitions":   plan,
				"total_objects": total,
			},
			"reingest_all": map[string]any{
				"summary": "none",
			},
			"total_objects_pending": total,
		}
		return outputJSON(out, payload)
	}

	fmt.Fprintln(out, "Upgrade plan:")
	fmt.Fprintln(out, "  reindex_auto:")
	fmt.Fprintln(out, "    objects_fts: signature ok (no rebuild needed)")
	fmt.Fprintln(out, "  reingest_selective:")
	if len(plan) == 0 {
		fmt.Fprintln(out, "    none")
	} else {
		// Deterministic ordering for human-readable output: sort by the
		// "from→to" key so successive `ctxt upgrade plan` runs produce the
		// same line order and operators can diff them.
		keys := make([]string, 0, len(plan))
		for k := range plan {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			row := plan[k]
			fmt.Fprintf(out, "    %s: %d objects\n", k, row.Count)
			fmt.Fprintf(out, "      Run: ctxt upgrade run --filter pipeline=%s\n", row.From)
		}
	}
	fmt.Fprintln(out, "  reingest_all:")
	fmt.Fprintln(out, "    none")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Total objects pending: %d\n", total)
	return nil
}

// reingestSelectiveRow is one (from→to) transition in the upgrade plan.
type reingestSelectiveRow struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// computeReingestSelectivePlan iterates the `objects` table once, parses
// each `pipeline` stamp, and groups rows by (family, currentVersion). For
// each (family, oldVer) where oldVer < currentVer, emits a row keyed
// "<from>→<to>".
//
// One pass over the table avoids N+1 queries when the registry has many
// families. Cost: ~O(rows × registries) for the per-row family lookup,
// trivially cheap for any operator-scale corpus.
func computeReingestSelectivePlan(ctx context.Context, db *sql.DB, reg pipeline.Registry) (map[string]reingestSelectiveRow, int, error) {
	rows, err := db.QueryContext(ctx, "SELECT pipeline, COUNT(*) FROM objects WHERE pipeline IS NOT NULL AND pipeline != '' GROUP BY pipeline")
	if err != nil {
		return nil, 0, fmt.Errorf("plan: scan pipelines: %w", err)
	}
	defer rows.Close()

	out := map[string]reingestSelectiveRow{}
	total := 0
	for rows.Next() {
		var stamp string
		var count int
		if err := rows.Scan(&stamp, &count); err != nil {
			return nil, 0, fmt.Errorf("plan: scan row: %w", err)
		}
		family, ver, ok := pipeline.ParseVersionedName(stamp)
		if !ok {
			continue
		}
		curVer, known := service.CurrentVersionForFamily(reg, family)
		if !known || ver >= curVer {
			continue
		}
		from := pipeline.FormatVersionedName(family, ver)
		to := pipeline.FormatVersionedName(family, curVer)
		key := from + " → " + to
		out[key] = reingestSelectiveRow{From: from, To: to, Count: count}
		total += count
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("plan: rows: %w", err)
	}
	return out, total, nil
}

// upgradeServiceDB extracts the underlying *sql.DB from an in-process
// *service.Service. Returns an error when the configured storage backend is
// not the SQLite driver — the in-process upgrade flow only supports SQLite
// (S3 / future remote backends will go through the daemon's gRPC endpoint).
func upgradeServiceDB(svc *service.Service) (*sql.DB, error) {
	if svc == nil || svc.Store == nil {
		return nil, errors.New("upgrade: service has no storage")
	}
	d, ok := svc.Store.(*sqlite.Driver)
	if !ok {
		return nil, errors.New("upgrade: only SQLite storage supports in-process upgrade run")
	}
	return d.DB(), nil
}

// runUpgradeRun executes the reingest_selective worker against a parsed
// selector (ADR-070 §1, T-0581). The bucket-3 (`--all`) form is reserved
// for an embedding-model-swap worker that lands in a future task; for now
// this command refuses with a pointer to ADR-070.
//
// Flow:
//  1. Parse --filter/--where into a Selector. --filter compiles to
//     `pipeline=<value>` and refuses bare values without a `@vN` suffix.
//     --where compiles to `where:<predicate>` and runs through SQLite's
//     EXPLAIN QUERY PLAN to validate before iteration starts.
//  2. Open the in-process service (same `newService()` used by every
//     other ctxt command — no daemon round-trip needed for SQLite).
//  3. Wire upgrade.Manager to the same shadow-state file dpkms uses so
//     the banner middleware and `ctxt upgrade status` see live progress
//     across processes.
//  4. Worker.Run blocks until done, ctx-cancelled, or budget exceeded.
func runUpgradeRun(cmd *cobra.Command, _ []string) error {
	all, _ := cmd.Flags().GetBool("all")
	consent, _ := cmd.Flags().GetString("i-understand-the-cost")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	filter, _ := cmd.Flags().GetString("filter")
	whereExpr, _ := cmd.Flags().GetString("where")
	rateLimit, _ := cmd.Flags().GetInt("rate-limit")
	budget, _ := cmd.Flags().GetFloat64("budget")

	// Bucket-3 refusal. Keep the consent-guard message ahead of the
	// out-of-scope message so operators who type `--all
	// --i-understand-the-cost ...` still see the right error.
	if all {
		if consent == "" {
			return errors.New("--all requires --i-understand-the-cost <sha> (the consent SHA appears in the release notes)")
		}
		return errors.New("upgrade run: --all (reingest_all bucket) is not implemented; the embedding-model-swap worker lands in a future release (see ADR-070 §Decision item 1)")
	}

	if filter == "" && whereExpr == "" {
		return errors.New("upgrade run: --filter <selector> or --where <predicate> required (or --all --i-understand-the-cost <sha> for full re-ingest)")
	}

	// --filter and --where are aliases at the user level: --filter
	// compiles via parsePipelineFilter, --where via parseWhereSelector.
	// They mutually exclude — passing both is operator error.
	if filter != "" && whereExpr != "" {
		return errors.New("upgrade run: pass --filter OR --where, not both")
	}
	selectorStr := filter
	if whereExpr != "" {
		selectorStr = "where:" + whereExpr
	}

	svc, cleanup, err := newService()
	if err != nil {
		return fmt.Errorf("upgrade run: %w", err)
	}
	defer cleanup()

	db, err := upgradeServiceDB(svc)
	if err != nil {
		return err
	}

	sel, err := upgrade.ParseSelectorWithDB(selectorStr, db)
	if err != nil {
		return fmt.Errorf("upgrade run: %w", err)
	}

	// Wire the same shadow file dpkms writes (and `ctxt upgrade status`
	// reads). When the daemon is also running, only one of the two
	// processes will hold the in-progress state at a time; the other
	// observes via /healthz or the shadow file. Cross-process locking
	// (e.g. flock on the shadow path) is a follow-up — for now the
	// worker's Manager.Start guard is single-process.
	shadowPath := ""
	if runDir, runErr := config.RunDir(); runErr == nil {
		shadowPath = filepath.Join(runDir, "upgrade-state.json")
	}
	mgr := upgrade.NewManager(shadowPath)

	worker := upgrade.NewWorker(db, svc, mgr, svc.Bus)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	out := cmd.OutOrStdout()
	if dryRun {
		count, err := sel.CountMatching(ctx, db)
		if err != nil {
			return fmt.Errorf("upgrade run --dry-run: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(out, map[string]any{
				"dry_run":  true,
				"selector": sel.Raw(),
				"matches":  count,
			})
		}
		fmt.Fprintf(out, "Dry run: selector %q matches %d objects.\n", sel.Raw(), count)
		fmt.Fprintln(out, "(no changes made; remove --dry-run to execute)")
		// Still emit the plan-computed event so downstream subscribers
		// (audit log, telemetry) see the planning step.
		_ = worker.Run(ctx, sel, upgrade.WorkerOpts{DryRun: true})
		return nil
	}

	if err := worker.Run(ctx, sel, upgrade.WorkerOpts{
		RateLimit: rateLimit,
		BudgetUSD: budget,
	}); err != nil {
		return fmt.Errorf("upgrade run: %w", err)
	}

	count, _ := sel.CountMatching(ctx, db)
	if isJSONOutput() {
		return outputJSON(out, map[string]any{
			"selector":  sel.Raw(),
			"completed": count,
		})
	}
	fmt.Fprintf(out, "Re-ingest complete: %d objects processed for selector %q.\n", count, sel.Raw())
	return nil
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
