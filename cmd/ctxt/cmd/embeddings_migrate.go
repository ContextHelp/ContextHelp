package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/migrate"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// migrateJobRetries is the retry budget of a migration job. A retry
// resumes from the rows already written.
const migrateJobRetries = 3

var embeddingsMigrateCmd = &cobra.Command{
	Use:   "migrate --to <model_id>",
	Short: "Fill a registered model's missing embeddings in the background",
	Long: `Queue a background migration that embeds every object missing a row for
<model_id>, using that model's provider only (ADR-071 "Migration job").

The migration runs in dpkms, not in this command: it keeps going after
ctxt exits, and a dpkms restart resumes it. It is restartable and
idempotent: work comes from the objects that still lack a row for the
model, so a re-run never embeds an object twice. An object the provider
rejects is recorded and skipped; re-run migrate to retry it.

The command targets the instance's database (--instance, or the current
instance); the dpkms serving that database runs the job. If none is
running, the job runs when it starts.

Flags:
  --to <model_id>     registered model to fill (required)
  --rate-limit N/s    cap provider calls per second (default: no cap)
  --batch N           objects per page (default 100, max 1000)
  --dry-run           count the missing objects; queue nothing

Progress: ctxt upgrade status (bucket embeddings_migrate); coverage:
ctxt embeddings list. Stop a run with dpkms job cancel <job-id>; re-run
migrate to continue.`,
	Args: cobra.NoArgs,
	RunE: runEmbeddingsMigrate,
}

func init() {
	embeddingsCmd.AddCommand(embeddingsMigrateCmd)
	f := embeddingsMigrateCmd.Flags()
	f.String("to", "", "registered model_id to fill")
	f.String("rate-limit", "0", "max provider calls per second, as N or N/s (0 = no cap)")
	f.Int("batch", migrate.DefaultBatch, fmt.Sprintf("objects per page (1..%d)", migrate.MaxBatch))

	cliconv.WithSideEffect(embeddingsMigrateCmd, cliconv.SideEffectWriteShared)
	// Re-running converges: an active job for the model is reported, not
	// duplicated, and a complete model queues nothing.
	cliconv.WithIdempotency(embeddingsMigrateCmd, cliconv.IdempotencyYes)

	cliconv.WithExamples(embeddingsMigrateCmd, []cliconv.Example{
		{Title: "Fill a candidate model's index", Command: "ctxt embeddings migrate --to ollama-nomic-embed-text@2026-09-26"},
		{Title: "Cap provider calls, JSON output", Command: "ctxt embeddings migrate --to ollama-nomic-embed-text@2026-09-26 --rate-limit 5/s --format json"},
		{Title: "Count what would be embedded", Command: "ctxt embeddings migrate --to ollama-nomic-embed-text@2026-09-26 --dry-run"},
	})
	cliconv.WithNextSteps(embeddingsMigrateCmd, []cliconv.NextStep{
		{When: "while it runs", Suggest: "ctxt upgrade status --watch", Reason: "follow progress and the failure count"},
		{When: "on completion", Suggest: "ctxt embeddings list", Reason: "check coverage reached 1.0 before flipping the default"},
	})
}

// Values of embeddingsMigrateDoc.Status.
const (
	migrateQueued        = "queued"
	migrateAlreadyQueued = "already_queued"
	migrateUpToDate      = "up_to_date"
	migrateDryRun        = "dry_run"
)

// embeddingsMigrateDoc is `ctxt embeddings migrate --format json`.
type embeddingsMigrateDoc struct {
	ModelID string `json:"model_id"`
	JobID   string `json:"job_id,omitempty"`
	// Status: queued, already_queued (an active job for the model exists;
	// JobID is it), up_to_date (nothing missing; no job), or dry_run.
	Status    string  `json:"status"`
	Missing   int     `json:"missing"`
	RateLimit float64 `json:"rate_limit,omitempty"`
	Batch     int     `json:"batch"`
}

func runEmbeddingsMigrate(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := migrateRequestFromFlags(cmd)
	if err != nil {
		return output.UsageError(err.Error()).Retaining(err)
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	reg, driver, cleanup, err := openEmbeddingsBackend()
	if err != nil {
		return err
	}
	defer cleanup()

	if _, err := migrate.Target(ctx, reg, req.ModelID, time.Now()); err != nil {
		return migrateTargetError(req.ModelID, err)
	}
	missing, err := migrate.CountMissing(ctx, driver.Embeddings(), req.ModelID)
	if err != nil {
		return fmt.Errorf("migrate %s: %w", req.ModelID, err)
	}

	doc := embeddingsMigrateDoc{ModelID: req.ModelID, Missing: missing, RateLimit: req.RateLimit, Batch: req.Batch}
	switch active, err := activeMigration(ctx, driver.Jobs(), req.ModelID); {
	case err != nil:
		return fmt.Errorf("migrate %s: %w", req.ModelID, err)
	case dryRun:
		doc.Status = migrateDryRun
	case active != nil:
		doc.Status, doc.JobID = migrateAlreadyQueued, active.ID
	case missing == 0:
		doc.Status = migrateUpToDate
	default:
		payload, err := req.Encode()
		if err != nil {
			return output.UsageError(err.Error()).Retaining(err)
		}
		job, err := jobs.NewQueue(driver.Jobs()).EnqueueTask(ctx, migrate.JobType, payload, migrateJobRetries)
		if err != nil {
			return fmt.Errorf("migrate %s: %w", req.ModelID, err)
		}
		doc.Status, doc.JobID = migrateQueued, job.ID
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	w := cmd.OutOrStdout()
	switch doc.Status {
	case migrateDryRun:
		fmt.Fprintf(w, "Dry run: %d objects lack a row for %s. Nothing queued.\n", missing, req.ModelID)
		return nil
	case migrateUpToDate:
		fmt.Fprintf(w, "Nothing to migrate: every object has a row for %s.\n", req.ModelID)
		return nil
	case migrateAlreadyQueued:
		fmt.Fprintf(w, "A migration to %s is already queued or running (job %s); not queuing another.\n", req.ModelID, doc.JobID)
	default:
		fmt.Fprintf(w, "Queued migration to %s (job %s): %d objects lack a row.\n", req.ModelID, doc.JobID, missing)
		fmt.Fprintln(w, "dpkms runs it in the background; it continues after this command exits.")
	}
	fmt.Fprintln(w, "Progress: ctxt upgrade status --watch")
	fmt.Fprintln(w, "Coverage: ctxt embeddings list")
	return nil
}

func migrateRequestFromFlags(cmd *cobra.Command) (migrate.Request, error) {
	to, _ := cmd.Flags().GetString("to")
	if to == "" {
		return migrate.Request{}, errors.New("--to <model_id> is required")
	}
	rawRate, _ := cmd.Flags().GetString("rate-limit")
	rate, err := migrate.ParseRate(rawRate)
	if err != nil {
		return migrate.Request{}, fmt.Errorf("--rate-limit: %w", err)
	}
	batch, _ := cmd.Flags().GetInt("batch")
	if batch < 1 {
		return migrate.Request{}, fmt.Errorf("--batch %d must be between 1 and %d", batch, migrate.MaxBatch)
	}
	req := migrate.Request{ModelID: to, RateLimit: rate, Batch: batch}
	if err := req.Validate(); err != nil {
		return migrate.Request{}, err
	}
	return req, nil
}

func migrateTargetError(modelID string, err error) error {
	switch {
	case errors.Is(err, registry.ErrModelNotFound):
		e := output.NotFoundError(fmt.Sprintf("migrate: %s is not registered", modelID))
		e.SuggestedFix = fmt.Sprintf("register it first: ctxt embeddings register %s --embedding-model <model>", modelID)
		return e.Retaining(err)
	case errors.Is(err, migrate.ErrNotMigratable):
		e := output.ConflictError(fmt.Sprintf("migrate: %v", err))
		e.SuggestedFix = "run `ctxt embeddings list` and migrate to a model that is registered, measured and not deprecated"
		return e.Retaining(err)
	default:
		return fmt.Errorf("migrate %s: %w", modelID, err)
	}
}

// activeMigration returns the pending or running migration job for
// modelID, if any.
func activeMigration(ctx context.Context, store storage.JobStore, modelID string) (*storage.Job, error) {
	for _, status := range []storage.JobStatus{storage.JobRunning, storage.JobPending} {
		list, _, err := store.List(ctx, storage.JobFilter{Type: migrate.JobType, Status: status})
		if err != nil {
			return nil, err
		}
		for _, j := range list {
			if req, err := migrate.Decode(j.Payload); err == nil && req.ModelID == modelID {
				return j, nil
			}
		}
	}
	return nil, nil
}
