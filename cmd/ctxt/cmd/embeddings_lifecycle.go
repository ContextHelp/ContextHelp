package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
	"hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/events"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/timeframe"
)

// The model lifecycle after register and migrate (ADR-071 "Default-flip
// control"): set-default promotes a covered model, deprecate schedules the
// old one's retirement, purge deletes it after the grace period. All three
// run against the instance's database (--instance or the current one).

var (
	// embeddingsNow is the lifecycle commands' clock. Tests pin it.
	embeddingsNow = time.Now
	// embeddingsEventBus builds the bus lifecycle events are published on:
	// kit's bus, which forwards to the sinks configured in the environment
	// (KIT_BUS_SINK). Tests subscribe to it.
	embeddingsEventBus = func() bus.Bus { return bus.New() }
)

var embeddingsSetDefaultCmd = &cobra.Command{
	Use:   "set-default <model_id>",
	Short: "Promote a registered model to the default, gated on coverage",
	Long: `Make <model_id> the default embedding model: the model every query embeds
with and searches.

The flip is refused, and nothing changes, unless the model's coverage (the
fraction of objects with vectors for it, as ctxt embeddings list shows it)
is at least the minimum: embeddings.min_coverage in config (default 0.99),
or --min-coverage for this call. A deprecated or unregistered model is
refused too. Fill a model's missing vectors with ctxt embeddings migrate.

The flip is one transaction: there is never more than one default, and a
concurrent flip waits for this one. Queries read the default on every
query, so the next query uses the new model with no restart. The previous
default keeps its vectors and keeps receiving new ones until it is
deprecated.

Promoting the current default changes nothing and succeeds.`,
	Args: cobra.ExactArgs(1),
	RunE: runEmbeddingsSetDefault,
}

var embeddingsDeprecateCmd = &cobra.Command{
	Use:   "deprecate <model_id>",
	Short: "Schedule a model's retirement",
	Long: `Schedule the retirement of <model_id> on the date given by --on (default:
now).

From that date ingest stops writing the model's vectors, and it can no
longer become the default. Its existing vectors and index stay until ctxt
embeddings purge deletes them, which is allowed once the grace period
(embeddings.grace_period, default 720h = 30 days) has passed after the
date.

--on takes a date (2026-10-01, the start of that day in local time), a
local date-time (2026-10-01T09:00:00) or an RFC 3339 time
(2026-10-01T09:00:00Z). It cannot be in the past: backdating would cut the
grace period short. A time earlier today counts as now.

The default model cannot be deprecated: promote its successor with ctxt
embeddings set-default first. Deprecating an already deprecated model
without --on keeps its date; with --on it reschedules.`,
	Args: cobra.ExactArgs(1),
	RunE: runEmbeddingsDeprecate,
}

var embeddingsPurgeCmd = &cobra.Command{
	Use:   "purge <model_id>",
	Short: "Delete a deprecated model's vectors, index and registry entry",
	Long: `Delete every embedding row of <model_id>, its vector index, its index
signature and its registry entry. This cannot be undone: the model's
vectors would have to be embedded again under a new registration.

Refused, deleting nothing, unless the model is deprecated and the grace
period (embeddings.grace_period, default 720h = 30 days) has passed since
its deprecation took effect. The default model is never purged.`,
	Args: cobra.ExactArgs(1),
	RunE: runEmbeddingsPurge,
}

func init() {
	embeddingsCmd.AddCommand(embeddingsSetDefaultCmd)
	embeddingsCmd.AddCommand(embeddingsDeprecateCmd)
	embeddingsCmd.AddCommand(embeddingsPurgeCmd)

	embeddingsSetDefaultCmd.Flags().Float64("min-coverage", config.DefaultEmbeddingsMinCoverage,
		"minimum coverage (0..1) the model needs; defaults to embeddings.min_coverage")
	embeddingsDeprecateCmd.Flags().String("on", "",
		"when the deprecation takes effect: YYYY-MM-DD, a local date-time or RFC 3339 (default now)")

	// set-default rewrites shared registry state; re-running converges.
	cliconv.WithSideEffect(embeddingsSetDefaultCmd, cliconv.SideEffectWriteShared)
	cliconv.WithIdempotency(embeddingsSetDefaultCmd, cliconv.IdempotencyYes)
	// deprecate ends a model's dual-write and starts its purge clock;
	// purge deletes vectors for good. Both take kit's typed confirmation
	// token. Re-running either changes nothing further.
	cliconv.WithSideEffect(embeddingsDeprecateCmd, cliconv.SideEffectDestructiveShared)
	cliconv.WithSideEffect(embeddingsPurgeCmd, cliconv.SideEffectDestructiveShared)
	cliconv.WithDestructiveToken(embeddingsDeprecateCmd)
	cliconv.WithDestructiveToken(embeddingsPurgeCmd)
	cliconv.WithIdempotency(embeddingsDeprecateCmd, cliconv.IdempotencyYes)
	cliconv.WithIdempotency(embeddingsPurgeCmd, cliconv.IdempotencyYes)

	cliconv.WithExamples(embeddingsSetDefaultCmd, []cliconv.Example{
		{Title: "Promote a fully migrated model", Command: "ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26"},
		{Title: "Accept lower coverage for this flip", Command: "ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26 --min-coverage 0.95"},
		{Title: "Check the guard without flipping", Command: "ctxt embeddings set-default ollama-snowflake-arctic-embed2@2026-09-26 --dry-run --format json"},
	})
	cliconv.WithNextSteps(embeddingsSetDefaultCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "verify the new default marker landed"},
		{When: "when refused for coverage", Suggest: "ctxt embeddings migrate --to <model_id>", Reason: "fill the model's missing vectors"},
		{When: "after verification", Suggest: "ctxt embeddings deprecate <old_model_id>", Reason: "stop dual-writing the previous default"},
	})
	cliconv.WithExamples(embeddingsDeprecateCmd, []cliconv.Example{
		{Title: "Retire the previous default now", Command: "ctxt embeddings deprecate ollama-nomic-embed-text@2026-09-26 --confirm-token=<sha>"},
		{Title: "Schedule retirement for a date", Command: "ctxt embeddings deprecate ollama-nomic-embed-text@2026-09-26 --on 2026-10-15 --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(embeddingsDeprecateCmd, []cliconv.NextStep{
		{When: "after the grace period", Suggest: "ctxt embeddings purge <model_id>", Reason: "delete the model's vectors and index"},
	})
	cliconv.WithExamples(embeddingsPurgeCmd, []cliconv.Example{
		{Title: "Delete a retired model", Command: "ctxt embeddings purge ollama-nomic-embed-text@2026-09-26 --confirm-token=<sha>"},
		{Title: "Count what would be deleted", Command: "ctxt embeddings purge ollama-nomic-embed-text@2026-09-26 --dry-run"},
	})
	cliconv.WithNextSteps(embeddingsPurgeCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "confirm the model is gone and the others are unaffected"},
	})
}

// embeddingsLifecycleConfig returns the validated embeddings block.
func embeddingsLifecycleConfig() (config.EmbeddingsConfig, error) {
	c := config.EmbeddingsConfig{
		MinCoverage: config.DefaultEmbeddingsMinCoverage,
		GracePeriod: config.DefaultEmbeddingsGracePeriod,
	}
	if cfg != nil {
		c = cfg.Embeddings
	}
	if err := c.Validate(); err != nil {
		return c, output.UsageError(err.Error()).Retaining(err)
	}
	return c, nil
}

// lifecycleModelID validates the model_id argument.
func lifecycleModelID(args []string) (string, error) {
	if err := storage.ValidateEmbeddingModelID(args[0]); err != nil {
		return "", output.UsageError(err.Error()).Retaining(err)
	}
	return args[0], nil
}

func lifecycleContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// rfc3339 formats t as the UTC RFC 3339 string lifecycle output uses.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// --- set-default -------------------------------------------------------------

// embeddingsSetDefaultDoc is `ctxt embeddings set-default --format json`.
type embeddingsSetDefaultDoc struct {
	ModelID string `json:"model_id"`
	// PreviousDefault is the default before the flip; null when there
	// was none. Equal to model_id when nothing changed.
	PreviousDefault *string `json:"previous_default"`
	Coverage        float64 `json:"coverage"`
	MinCoverage     float64 `json:"min_coverage"`
	// Changed is false when the model already was the default.
	Changed bool `json:"changed"`
	DryRun  bool `json:"dry_run"`
}

func runEmbeddingsSetDefault(cmd *cobra.Command, args []string) error {
	ctx := lifecycleContext(cmd)
	modelID, err := lifecycleModelID(args)
	if err != nil {
		return err
	}
	lc, err := embeddingsLifecycleConfig()
	if err != nil {
		return err
	}
	minCoverage := lc.MinCoverage
	if cmd.Flags().Changed("min-coverage") {
		minCoverage, _ = cmd.Flags().GetFloat64("min-coverage")
		if minCoverage < 0 || minCoverage > 1 {
			return output.UsageError(fmt.Sprintf("--min-coverage %v must be a fraction between 0 and 1", minCoverage))
		}
	}
	dryRun := kitcli.IsDryRun(cmd)

	reg, cleanup, err := newEmbeddingsRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	flip := reg.SetDefault
	if dryRun {
		flip = reg.PlanSetDefault
	}
	p, err := flip(ctx, modelID, minCoverage)
	if err != nil {
		return lifecycleError("set-default", modelID, err)
	}

	doc := embeddingsSetDefaultDoc{
		ModelID: modelID, Coverage: p.Coverage, MinCoverage: p.MinCoverage, Changed: p.Changed, DryRun: dryRun,
	}
	if p.Previous != "" {
		prev := p.Previous
		doc.PreviousDefault = &prev
	}
	if p.Changed && !dryRun {
		publishLifecycleEvent(cmd, events.TopicCtxtUpgradeEmbeddingModelPromoted, events.EmbeddingModelLifecyclePayload{
			ModelID: modelID, PreviousDefault: p.Previous, Coverage: &p.Coverage, MinCoverage: &p.MinCoverage,
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	w := cmd.OutOrStdout()
	prev := p.Previous
	if prev == "" {
		prev = "no default"
	}
	switch {
	case !p.Changed:
		fmt.Fprintf(w, "%s is already the default embedding model. Nothing changed.\n", modelID)
		return nil
	case dryRun:
		fmt.Fprintf(w, "Dry run: would make %s the default embedding model (currently %s). Nothing changed.\n", modelID, prev)
	default:
		fmt.Fprintf(w, "Default embedding model is now %s (was %s).\n", modelID, prev)
	}
	fmt.Fprintf(w, "Coverage: %.4f (minimum %.4f)\n", p.Coverage, p.MinCoverage)
	if p.Previous != "" && !dryRun {
		fmt.Fprintf(w, "The previous default keeps receiving vectors until deprecated: ctxt embeddings deprecate %s\n", p.Previous)
	}
	return nil
}

// --- deprecate -----------------------------------------------------------------

// embeddingsDeprecateDoc is `ctxt embeddings deprecate --format json`.
type embeddingsDeprecateDoc struct {
	ModelID      string `json:"model_id"`
	DeprecatedAt string `json:"deprecated_at"`
	// PurgeEligibleAt is DeprecatedAt plus GracePeriod.
	PurgeEligibleAt string `json:"purge_eligible_at"`
	GracePeriod     string `json:"grace_period"`
	// Changed is false when the model already was deprecated and --on
	// was not given.
	Changed bool `json:"changed"`
	DryRun  bool `json:"dry_run"`
}

func runEmbeddingsDeprecate(cmd *cobra.Command, args []string) error {
	ctx := lifecycleContext(cmd)
	modelID, err := lifecycleModelID(args)
	if err != nil {
		return err
	}
	lc, err := embeddingsLifecycleConfig()
	if err != nil {
		return err
	}
	now := embeddingsNow()
	var on time.Time
	if cmd.Flags().Changed("on") {
		raw, _ := cmd.Flags().GetString("on")
		if on, err = parseDeprecateOn(raw, now); err != nil {
			return err
		}
	}
	dryRun := kitcli.IsDryRun(cmd)

	reg, cleanup, err := newEmbeddingsRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	m, err := reg.Get(ctx, modelID)
	if err != nil {
		return lifecycleError("deprecate", modelID, err)
	}
	changed := true
	switch {
	case !on.IsZero():
	case m.DeprecatedAt != nil:
		on, changed = *m.DeprecatedAt, false
	default:
		on = now
	}
	on = on.UTC().Truncate(time.Second)
	if changed {
		deprecate := reg.Deprecate
		if dryRun {
			deprecate = reg.PlanDeprecate
		}
		if err := deprecate(ctx, modelID, on); err != nil {
			return lifecycleError("deprecate", modelID, err)
		}
	}

	eligible := on.Add(lc.GracePeriod)
	doc := embeddingsDeprecateDoc{
		ModelID: modelID, DeprecatedAt: rfc3339(on), PurgeEligibleAt: rfc3339(eligible),
		GracePeriod: lc.GracePeriod.String(), Changed: changed, DryRun: dryRun,
	}
	if changed && !dryRun {
		publishLifecycleEvent(cmd, events.TopicCtxtUpgradeEmbeddingModelDeprecated, events.EmbeddingModelLifecyclePayload{
			ModelID: modelID, DeprecatedAt: doc.DeprecatedAt, PurgeEligibleAt: doc.PurgeEligibleAt,
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	w := cmd.OutOrStdout()
	switch {
	case !changed:
		fmt.Fprintf(w, "%s is already deprecated from %s. Nothing changed.\n", modelID, doc.DeprecatedAt)
	case dryRun:
		fmt.Fprintf(w, "Dry run: would deprecate %s from %s. Nothing changed.\n", modelID, doc.DeprecatedAt)
	default:
		fmt.Fprintf(w, "Deprecated %s from %s: ingest stops writing its vectors then.\n", modelID, doc.DeprecatedAt)
	}
	fmt.Fprintf(w, "Purge allowed from %s (grace period %s): ctxt embeddings purge %s\n", doc.PurgeEligibleAt, doc.GracePeriod, modelID)
	return nil
}

// parseDeprecateOn parses --on in the repo's time grammar (a bare date, a
// local date-time or RFC 3339). A deprecation cannot be backdated, since
// that would shorten the purge grace period: a value before the start of
// today is refused, and one earlier today counts as now.
func parseDeprecateOn(raw string, now time.Time) (time.Time, error) {
	loc := time.Local
	t, err := timeframe.ParseTime(raw, now, loc)
	if err != nil {
		return time.Time{}, output.UsageError(fmt.Sprintf(
			"--on %q: use a date (2026-10-01), a local date-time (2026-10-01T09:00:00) or an RFC 3339 time", raw,
		)).Retaining(err)
	}
	n := now.In(loc)
	today := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, loc)
	if t.Before(today) {
		return time.Time{}, output.UsageError(fmt.Sprintf(
			"--on %q is in the past (%s); a deprecation cannot be backdated, since that would shorten the purge grace period",
			raw, rfc3339(t),
		))
	}
	if t.Before(now) {
		t = now
	}
	return t, nil
}

// --- purge ---------------------------------------------------------------------

// embeddingsPurgeDoc is `ctxt embeddings purge --format json`.
type embeddingsPurgeDoc struct {
	ModelID         string `json:"model_id"`
	DeprecatedAt    string `json:"deprecated_at"`
	PurgeEligibleAt string `json:"purge_eligible_at"`
	// Rows is the number of embedding rows deleted (or, on a dry run,
	// that would be).
	Rows   int64 `json:"rows"`
	DryRun bool  `json:"dry_run"`
}

func runEmbeddingsPurge(cmd *cobra.Command, args []string) error {
	ctx := lifecycleContext(cmd)
	modelID, err := lifecycleModelID(args)
	if err != nil {
		return err
	}
	lc, err := embeddingsLifecycleConfig()
	if err != nil {
		return err
	}
	now := embeddingsNow()
	dryRun := kitcli.IsDryRun(cmd)

	reg, driver, cleanup, err := openEmbeddingsBackend()
	if err != nil {
		return err
	}
	defer cleanup()

	var p registry.Purged
	if dryRun {
		p, err = reg.PlanPurge(ctx, modelID, lc.GracePeriod, now)
	} else {
		p, err = reg.Purge(ctx, driver.Embeddings(), modelID, lc.GracePeriod, now)
	}
	if err != nil {
		return lifecycleError("purge", modelID, err)
	}

	doc := embeddingsPurgeDoc{
		ModelID: modelID, DeprecatedAt: rfc3339(p.DeprecatedAt), PurgeEligibleAt: rfc3339(p.EligibleAt),
		Rows: p.Rows, DryRun: dryRun,
	}
	if !dryRun {
		publishLifecycleEvent(cmd, events.TopicCtxtUpgradeEmbeddingModelPurged, events.EmbeddingModelLifecyclePayload{
			ModelID: modelID, DeprecatedAt: doc.DeprecatedAt, PurgeEligibleAt: doc.PurgeEligibleAt, Rows: &p.Rows,
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	w := cmd.OutOrStdout()
	if dryRun {
		fmt.Fprintf(w, "Dry run: would delete %d embedding rows of %s, its index and its registry entry. Nothing changed.\n", p.Rows, modelID)
		return nil
	}
	fmt.Fprintf(w, "Purged %s: deleted %d embedding rows, its index and its registry entry.\n", modelID, p.Rows)
	return nil
}

// --- shared --------------------------------------------------------------------

// publishLifecycleEvent emits a lifecycle event after the change committed.
// A publish failure is reported, not returned: the change stands.
func publishLifecycleEvent(cmd *cobra.Command, topic bus.Topic, payload events.EmbeddingModelLifecyclePayload) {
	ctx := lifecycleContext(cmd)
	b := embeddingsEventBus()
	defer func() { _ = b.Close(ctx) }()
	if err := events.NewPublisher(b).Publish(ctx, topic, payload); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: publish %s: %v\n", topic, err)
	}
}

// lifecycleError maps registry refusals to kit error classes with a fix.
func lifecycleError(verb, modelID string, err error) error {
	var cov *registry.CoverageError
	var grace *registry.GraceError
	var e *output.Error
	switch {
	case errors.Is(err, registry.ErrModelNotFound):
		e = output.NotFoundError(fmt.Sprintf("%s: %s is not registered", verb, modelID))
		e.SuggestedFix = "run `ctxt embeddings list` to see the registered models"
	case errors.As(err, &cov):
		e = output.ConflictError(fmt.Sprintf(
			"set-default: %s covers %.2f of the corpus, below the minimum %.2f; the default is unchanged",
			modelID, cov.Coverage, cov.MinCoverage,
		))
		e.SuggestedFix = fmt.Sprintf("fill its missing vectors with `ctxt embeddings migrate --to %s`, or pass --min-coverage to accept less for this flip", modelID)
	case errors.Is(err, registry.ErrModelDeprecated):
		e = output.ConflictError(fmt.Sprintf("set-default: %s is deprecated and cannot become the default", modelID))
		e.SuggestedFix = "promote a model that is not deprecated; run `ctxt embeddings list`"
	case errors.Is(err, registry.ErrModelNotMeasured):
		e = output.ConflictError(fmt.Sprintf("set-default: %s has no measured dimension, so it has no index to search", modelID))
		e.SuggestedFix = "register the model under a new model_id with `ctxt embeddings register`, which measures its dimension"
	case errors.Is(err, registry.ErrIsDefault):
		e = output.ConflictError(fmt.Sprintf("%s: %s is the default embedding model", verb, modelID))
		e.SuggestedFix = "promote its successor first: ctxt embeddings set-default <model_id>"
	case errors.Is(err, registry.ErrNotDeprecated):
		e = output.ConflictError(fmt.Sprintf("purge: %s is not deprecated", modelID))
		e.SuggestedFix = fmt.Sprintf("deprecate it first (ctxt embeddings deprecate %s), then purge after the grace period", modelID)
	case errors.As(err, &grace):
		e = output.ConflictError(fmt.Sprintf(
			"purge: %s was deprecated at %s; its grace period ends at %s, so nothing was deleted",
			modelID, rfc3339(grace.DeprecatedAt), rfc3339(grace.EligibleAt),
		))
		e.SuggestedFix = fmt.Sprintf("run purge again after %s, or shorten embeddings.grace_period", rfc3339(grace.EligibleAt))
	default:
		return fmt.Errorf("%s %s: %w", verb, modelID, err)
	}
	return e.Retaining(err)
}
