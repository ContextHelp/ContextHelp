package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// zombieThreshold: jobs stuck in "running" for longer than this are zombies.
const zombieThreshold = 2 * time.Hour

// renderPreview writes a maintenance preview: the structured plan when
// the caller asked for a machine format, and human prose otherwise.
//
// The two renderings carry the same facts in the two registers their
// readers actually use. A program approving a reclaim pass needs the
// plan's fields; an operator at a terminal has always been shown these
// sentences, and rewriting that view is a regression rather than a fix
// — so the prose is passed in as a closure and kept verbatim.
//
// Both go to cmd.OutOrStdout(): a preview IS the result of a preview
// invocation, so it belongs on stdout. That is the opposite of the
// progress narration a real housekeeping run emits, which is chatter
// about work in flight and goes to stderr so it cannot interleave with
// a --format json document.
//
// The plan is taken by value: callers build one inline at the call
// site, and a pointer parameter would only push the same copy out to
// each of them. gocritic's hugeParam advisory is accepted here — 80
// bytes once per preview invocation is not a cost worth contorting
// five call sites for.
//
//nolint:gocritic // hugeParam: one 80-byte copy per preview; see above.
func renderPreviewPlan(cmd *cobra.Command, plan cliformat.Plan, human func(*lineWriter)) error {
	w := cmd.OutOrStdout()
	if !cliformat.Structured() {
		lw := &lineWriter{w: w}
		human(lw)
		return lw.err
	}
	return cliformat.EncodePreview(w, plan)
}

// lineWriter writes prose lines and remembers the first write error.
//
// A preview's job is to be read, so a half-written one is a failure
// the caller must hear about: a truncated plan that still exits 0
// reads as "this would change less than it would". Threading a check
// through every Fprintf at the call sites would bury the prose it is
// there to render, so the error is accumulated here and returned once.
type lineWriter struct {
	w   io.Writer
	err error
}

// printf writes one formatted line, keeping the first error.
func (l *lineWriter) printf(format string, args ...any) {
	if l.err != nil {
		return
	}
	_, l.err = fmt.Fprintf(l.w, format, args...)
}

// println writes one line, keeping the first error.
func (l *lineWriter) println(args ...any) {
	if l.err != nil {
		return
	}
	_, l.err = fmt.Fprintln(l.w, args...)
}

func openDB() (*sqlite.Driver, func(), error) {
	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = "sqlite"
	}
	if storageType != "sqlite" {
		return nil, nil, fmt.Errorf("housekeeping only supports sqlite storage (got %s)", storageType)
	}
	storagePath := cfg.Storage.Path
	if storagePath == "" {
		return nil, nil, fmt.Errorf("storage path not configured")
	}

	driver, err := sqlite.New(storagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	ctx := context.Background()
	if err := driver.Init(ctx); err != nil {
		driver.Close(ctx)
		return nil, nil, fmt.Errorf("init database: %w", err)
	}
	cleanup := func() { driver.Close(context.Background()) }
	return driver, cleanup, nil
}

var housekeepingCmd = &cobra.Command{
	Use:   "housekeeping",
	Short: "Database maintenance and optimization",
	Long: `Perform database maintenance operations including:
  - Vacuum: Reclaim storage space
  - Reindex: Rebuild search indexes
  - Compact: Optimize database file
  - Prune: Remove old data

Examples:
  # Vacuum the database
  dpkms housekeeping vacuum

  # Rebuild indexes
  dpkms housekeeping reindex

  # Compact database
  dpkms housekeeping compact

  # Prune data older than a date
  dpkms housekeeping prune --before 2024-01-01`,
}

var vacuumCmd = &cobra.Command{
	Use:   "vacuum",
	Short: "Reclaim storage space",
	Long:  `Run VACUUM on the database to reclaim unused storage space.`,
	RunE:  runVacuum,
}

var reindexCmd = &cobra.Command{
	Use:   "reindex",
	Short: "Rebuild search indexes",
	Long:  `Rebuild FTS and vector indexes for optimal search performance.`,
	RunE:  runReindex,
}

var compactCmd = &cobra.Command{
	Use:   "compact",
	Short: "Optimize database file",
	Long:  `Compact and optimize the database file.`,
	RunE:  runCompact,
}

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove old data",
	Long:  `Remove data older than the specified date.`,
	RunE:  runPrune,
}

var runAllCmd = &cobra.Command{
	Use:   "run",
	Short: "Run all maintenance tasks",
	Long: `Run all maintenance tasks in sequence:
  1. WAL checkpoint
  2. VACUUM SQLite DB
  3. Purge orphaned blobs
  4. Compact embeddings table
  5. Prune jobs history >30 days
  6. Detect zombie jobs (running but stale)
  7. Print stats summary`,
	RunE: runAll,
}

func init() {
	rootCmd.AddCommand(housekeepingCmd)

	// Add subcommands
	housekeepingCmd.AddCommand(vacuumCmd)
	housekeepingCmd.AddCommand(reindexCmd)
	housekeepingCmd.AddCommand(compactCmd)
	housekeepingCmd.AddCommand(pruneCmd)
	housekeepingCmd.AddCommand(runAllCmd)

	// Prune flags. --before is required for a prune that deletes and
	// optional for one that only previews; see pruneCutoff. Cobra's
	// MarkFlagRequired fires in ValidateRequiredFlags, before RunE, so
	// the exemption has to be applied there rather than inside the
	// handler — hence the PreRunE below instead of MarkFlagRequired.
	pruneCmd.Flags().String("before", "",
		"remove data before this date (ISO format); required unless --dry-run")
	pruneCmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if kitcli.IsDryRun(cmd) {
			return nil
		}
		if strings.TrimSpace(viper.GetString("housekeeping.before")) == "" {
			return output.UsageError(`required flag(s) "before" not set`)
		}
		return nil
	}

	// run flags. --dry-run is NOT declared here: kit owns it as a root
	// persistent flag, and a local re-declaration would shadow the
	// inherited one and break the kit.dry_run viper binding. Readers
	// use kitcli.IsDryRun(cmd).
	runAllCmd.Flags().Duration("zombie-threshold", zombieThreshold,
		"jobs running longer than this are considered zombies")

	// Side-effect tiers. Kit refuses --dry-run on any leaf that has not
	// declared one, and uses the tier to decide whether the confirmation
	// gate applies. Every leaf below mutates the SQLite store in place.
	//
	// vacuum/compact rewrite the database file wholesale, prune issues
	// row deletes, reindex discards and rebuilds the FTS index, and run
	// does all of the above plus a blob purge. None has an inverse
	// command that restores prior state, so all five are Destructive.
	cliconv.WithSideEffect(vacuumCmd, cliconv.SideEffectDestructive)
	cliconv.WithSideEffect(reindexCmd, cliconv.SideEffectDestructive)
	cliconv.WithSideEffect(compactCmd, cliconv.SideEffectDestructive)
	cliconv.WithSideEffect(pruneCmd, cliconv.SideEffectDestructive)
	cliconv.WithSideEffect(runAllCmd, cliconv.SideEffectDestructive)

	// Bind flags to viper
	viper.BindPFlag("housekeeping.before", pruneCmd.Flags().Lookup("before"))
}

func runVacuum(cmd *cobra.Command, args []string) error {
	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	if kitcli.IsDryRun(cmd) {
		free, total, pageSize, err := dbPageStats(ctx, driver.DB())
		if err != nil {
			return err
		}
		return renderPreviewPlan(cmd, cliformat.NewPlan(cmd.CommandPath(), []cliformat.Action{{
			Kind:       "vacuum",
			Target:     "database:" + cfg.Storage.Path,
			Count:      free,
			Reversible: false,
			Detail: fmt.Sprintf("reclaim %d KB of %d KB (%d of %d pages free)",
				free*pageSize/1024, total*pageSize/1024, free, total),
		}}), func(w *lineWriter) {
			w.println("[dry-run] no writes will occur")
			w.printf("Would run VACUUM on database: %s\n", cfg.Storage.Path)
			w.printf("  Pages total    : %d (%d KB)\n", total, total*pageSize/1024)
			w.printf("  Pages free     : %d (%d KB reclaimable)\n", free, free*pageSize/1024)
		})
	}

	fmt.Println("Running VACUUM on database...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	if _, err := driver.DB().ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	fmt.Println("Vacuum completed")
	return nil
}

// dbPageStats reads SQLite page accounting without mutating the store.
// The vacuum and compact previews use it to report reclaimable space.
// All three PRAGMAs are read-only.
func dbPageStats(ctx context.Context, db *sql.DB) (free, total, pageSize int64, err error) {
	if err = db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&free); err != nil {
		return 0, 0, 0, fmt.Errorf("freelist_count: %w", err)
	}
	if err = db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&total); err != nil {
		return 0, 0, 0, fmt.Errorf("page_count: %w", err)
	}
	if err = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		return 0, 0, 0, fmt.Errorf("page_size: %w", err)
	}
	return free, total, pageSize, nil
}

func runReindex(cmd *cobra.Command, args []string) error {
	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	db := driver.DB()

	if kitcli.IsDryRun(cmd) {
		var objects int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM objects").Scan(&objects); err != nil {
			return fmt.Errorf("count objects: %w", err)
		}
		return renderPreviewPlan(cmd, cliformat.NewPlan(cmd.CommandPath(), []cliformat.Action{
			{
				Kind:       "reindex",
				Target:     "table:objects_fts",
				Count:      objects,
				Reversible: false,
				Detail:     "discard and rebuild the full-text index",
			},
			{
				Kind:       "optimize",
				Target:     "database:" + cfg.Storage.Path,
				Count:      0,
				Reversible: true,
				Detail:     "PRAGMA optimize",
			},
		}), func(w *lineWriter) {
			w.println("[dry-run] no writes will occur")
			w.printf("Would rebuild search indexes on: %s\n", cfg.Storage.Path)
			w.printf("  Objects to index: %d\n", objects)
			w.println("  Targets         : objects_fts (rebuild), PRAGMA optimize")
		})
	}

	fmt.Println("Rebuilding search indexes...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	if _, err := db.ExecContext(ctx, "INSERT INTO objects_fts(objects_fts) VALUES('rebuild')"); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: FTS rebuild: %v\n", err)
	} else {
		fmt.Println("  FTS indexes rebuilt")
	}

	if _, err := db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		fmt.Fprintf(os.Stderr, "  warning: optimize: %v\n", err)
	}

	fmt.Println("\nReindexing completed")
	return nil
}

func runCompact(cmd *cobra.Command, args []string) error {
	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	db := driver.DB()

	if kitcli.IsDryRun(cmd) {
		free, total, pageSize, err := dbPageStats(ctx, db)
		if err != nil {
			return err
		}
		return renderPreviewPlan(cmd, cliformat.NewPlan(cmd.CommandPath(), []cliformat.Action{
			{
				Kind:       "optimize",
				Target:     "database:" + cfg.Storage.Path,
				Count:      0,
				Reversible: true,
				Detail:     "PRAGMA optimize",
			},
			{
				Kind:       "vacuum",
				Target:     "database:" + cfg.Storage.Path,
				Count:      free,
				Reversible: false,
				Detail: fmt.Sprintf("reclaim %d KB of %d KB (%d of %d pages free)",
					free*pageSize/1024, total*pageSize/1024, free, total),
			},
		}), func(w *lineWriter) {
			w.println("[dry-run] no writes will occur")
			w.printf("Would compact database: %s\n", cfg.Storage.Path)
			w.printf("  Pages total    : %d (%d KB)\n", total, total*pageSize/1024)
			w.printf("  Pages free     : %d (%d KB reclaimable)\n", free, free*pageSize/1024)
			w.println("  Targets        : PRAGMA optimize, VACUUM")
		})
	}

	fmt.Println("Compacting database...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	if _, err := db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("optimize: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	fmt.Println("Compaction completed")
	return nil
}

// defaultPruneRetention is the cutoff a prune preview assumes when the
// caller named none. It matches the 30-day jobs-history window the
// full housekeeping pass applies, so previewing the step alone
// projects the same work the composite pass would do.
const defaultPruneRetention = 30 * 24 * time.Hour

// pruneCutoff resolves the --before date for this invocation.
//
// --before stays mandatory for a prune that actually deletes: choosing
// a cutoff is the whole decision, and inferring one on the operator's
// behalf would delete rows they never named. A preview is the opposite
// case. Asking "what would this remove" is how a caller DISCOVERS
// which cutoff it wants, so requiring the answer up front makes the
// destructive step the one thing that cannot be previewed — exactly
// the gap the maintenance contract calls out. Under --dry-run the
// default retention window applies and the plan reports which cutoff
// it used, so nothing is inferred silently.
func pruneCutoff(cmd *cobra.Command) (time.Time, string, error) {
	before := viper.GetString("housekeeping.before")
	if before == "" && kitcli.IsDryRun(cmd) {
		t := time.Now().Add(-defaultPruneRetention)
		return t, t.Format("2006-01-02"), nil
	}
	t, err := time.Parse("2006-01-02", before)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid date format (expected YYYY-MM-DD): %w", err)
	}
	return t, before, nil
}

func runPrune(cmd *cobra.Command, args []string) error {
	beforeTime, before, err := pruneCutoff(cmd)
	if err != nil {
		return err
	}

	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	filter := storage.ObjectFilter{
		Before: &beforeTime,
		Limit:  10000,
	}
	objects, total, err := driver.Objects().List(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	// The preview answers before the empty-result shortcut below: a
	// plan with no actions is still a plan, and a caller that asked
	// for JSON must get a document rather than the sentence "No
	// objects found", whose only reading is by eye.
	if kitcli.IsDryRun(cmd) {
		actions := make([]cliformat.Action, 0, len(objects))
		for _, obj := range objects {
			actions = append(actions, cliformat.Action{
				Kind:       "delete",
				Target:     "object:" + obj.ID,
				Count:      1,
				Reversible: false,
				Detail:     "created before " + before,
			})
		}
		return renderPreviewPlan(cmd, cliformat.NewPlan(cmd.CommandPath(), actions), func(w *lineWriter) {
			if total == 0 {
				w.printf("No objects found before %s.\n", before)
				return
			}
			w.printf("Found %d objects before %s.\n", total, before)
			w.println("[dry-run] no writes will occur")
			w.printf("Would delete %d objects from: %s\n", total, cfg.Storage.Path)
			for _, obj := range objects {
				w.printf("  delete object %s\n", obj.ID)
			}
		})
	}

	if total == 0 {
		fmt.Printf("No objects found before %s.\n", before)
		return nil
	}

	fmt.Printf("Found %d objects before %s.\n", total, before)

	fmt.Print("Proceed with deletion? (y/N): ")
	var response string
	fmt.Scanln(&response)
	if response != "y" && response != "Y" {
		fmt.Println("Pruning cancelled.")
		return nil
	}

	deleted := 0
	for _, obj := range objects {
		if err := driver.Objects().Delete(ctx, obj.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: delete %s: %v\n", obj.ID, err)
			continue
		}
		deleted++
	}
	fmt.Printf("\nPruned %d objects\n", deleted)
	return nil
}

// housekeepingStats collects counters during a full maintenance run.
type housekeepingStats struct {
	PagesBefore   int64
	PagesAfter    int64
	PageSize      int64
	JobsPruned    int64
	ZombiesFound  int64
	ZombiesReset  int64
	OrphanBlobs   int64
	EmbeddingsOpt bool
}

// progress writes a narration line to stderr. A full housekeeping run
// is chatty, and that chatter is diagnostic, not the result: it must
// not land on stdout, where it would interleave with — and invalidate —
// the structured document a --format json caller is parsing.
func progress(line string) {
	fmt.Fprintln(os.Stderr, line)
}

// summary returns the machine-readable form of a housekeeping run:
// the same counters print() renders, with the two derived figures
// precomputed so a caller need not know the page arithmetic.
func (s housekeepingStats) summary() map[string]any {
	return map[string]any{
		"db_size_mb":       s.PagesAfter * s.PageSize / (1024 * 1024),
		"reclaimed_kb":     (s.PagesBefore - s.PagesAfter) * s.PageSize / 1024,
		"jobs_pruned":      s.JobsPruned,
		"zombies_found":    s.ZombiesFound,
		"zombies_reset":    s.ZombiesReset,
		"orphan_blobs":     s.OrphanBlobs,
		"embeddings_optim": s.EmbeddingsOpt,
	}
}

// plan projects the full maintenance pass as the ordered list of
// actions a real run would carry out.
//
// One action per mutating step, in execution order, and exactly the
// six the command's own help documents: checkpoint, vacuum, optimize
// embeddings, purge orphan blobs, prune jobs history, reset zombies.
// The counts come from the same read-only queries the dry path already
// ran, so the projection reports real magnitudes rather than
// placeholders — which is what makes it reviewable. The seventh step
// the help lists, "print stats summary", is not an action: it changes
// nothing, and padding the plan with it would make the list disagree
// with the work.
func (s housekeepingStats) plan(command string, threshold time.Duration) cliformat.Plan {
	return cliformat.NewPlan(command, []cliformat.Action{
		{
			Kind: "checkpoint", Target: "wal:" + cfg.Storage.Path, Count: 0,
			Reversible: true, Detail: "PRAGMA wal_checkpoint(TRUNCATE)",
		},
		{
			Kind: "vacuum", Target: "database:" + cfg.Storage.Path, Count: 0,
			Reversible: false,
			Detail: fmt.Sprintf("VACUUM; %d pages of %d KB currently allocated",
				s.PagesBefore, s.PagesBefore*s.PageSize/1024),
		},
		{
			Kind: "optimize", Target: "table:embeddings_fts", Count: 0,
			Reversible: true, Detail: "optimize the embeddings index if the table exists",
		},
		{
			Kind: "delete", Target: "table:blobs", Count: s.OrphanBlobs,
			Reversible: false, Detail: "purge blobs whose object no longer exists",
		},
		{
			Kind: "delete", Target: "table:jobs", Count: s.JobsPruned,
			Reversible: false, Detail: "prune completed, failed and canceled jobs older than 30 days",
		},
		{
			Kind: "update", Target: "table:jobs", Count: s.ZombiesFound,
			Reversible: false,
			Detail:     fmt.Sprintf("reset jobs running longer than %s back to pending", threshold),
		},
	})
}

// print writes the human summary of a completed pass.
//
// It takes a writer rather than calling fmt.Print so the summary lands
// on the command's own stdout. The package-level fmt.Print it used to
// call wrote past cmd.OutOrStdout(), which the in-process test
// harnesses redirect — so a redirected run saw an empty summary. The
// writer carries the first write error rather than dropping it.
func (s housekeepingStats) print(w *lineWriter) {
	reclaimedKB := (s.PagesBefore - s.PagesAfter) * s.PageSize / 1024
	currentMB := s.PagesAfter * s.PageSize / (1024 * 1024)

	w.println("\n--- Housekeeping Summary ---")
	w.printf("  DB size now    : %d MB\n", currentMB)
	w.printf("  Space reclaimed: %d KB\n", reclaimedKB)
	w.printf("  Jobs pruned    : %d\n", s.JobsPruned)
	w.printf("  Zombie jobs    : %d found, %d reset to pending\n", s.ZombiesFound, s.ZombiesReset)
	w.printf("  Orphan blobs   : %d\n", s.OrphanBlobs)
	if s.EmbeddingsOpt {
		w.println("  Embeddings     : optimized")
	} else {
		w.println("  Embeddings     : skipped (table absent)")
	}
	w.println("----------------------------")
}

func runAll(cmd *cobra.Command, _ []string) error {
	threshold, _ := cmd.Flags().GetDuration("zombie-threshold")
	dryRun := kitcli.IsDryRun(cmd)

	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	db := driver.DB()
	stats := housekeepingStats{}

	if dryRun {
		progress("[dry-run] no writes will occur")
	}

	// 1. Page count before.
	_ = db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PagesBefore)
	_ = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&stats.PageSize)

	// 2. WAL checkpoint.
	progress("Checkpointing WAL...")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: wal_checkpoint: %v\n", err)
		}
	}
	progress("done")

	// 3. VACUUM.
	progress("Running VACUUM...")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
			return fmt.Errorf("vacuum: %w", err)
		}
	}
	progress("done")

	// Page count after vacuum.
	_ = db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PagesAfter)

	// 4. Compact / optimize embeddings table (best-effort).
	progress("Optimizing embeddings...")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "INSERT INTO embeddings_fts(embeddings_fts) VALUES('optimize')"); err == nil {
			stats.EmbeddingsOpt = true
		}
	}
	progress("done")

	// 5. Orphaned blobs: blobs whose object_id references no known object.
	progress("Purging orphaned blobs...")
	if !dryRun {
		res, err := db.ExecContext(ctx, `DELETE FROM blobs WHERE object_id NOT IN (SELECT id FROM objects)`)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: purge blobs: %v\n", err)
		} else {
			stats.OrphanBlobs, _ = res.RowsAffected()
		}
	} else {
		_ = db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM blobs WHERE object_id NOT IN (SELECT id FROM objects)`).
			Scan(&stats.OrphanBlobs)
	}
	progress("done")

	// 6. Prune jobs history >30 days.
	progress("Pruning old jobs...")
	cutoff := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	if !dryRun {
		res, err := db.ExecContext(ctx,
			"DELETE FROM jobs WHERE status IN ('completed','failed','cancelled') AND updated_at < ?",
			cutoff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: prune jobs: %v\n", err)
		} else {
			stats.JobsPruned, _ = res.RowsAffected()
		}
	} else {
		_ = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM jobs WHERE status IN ('completed','failed','cancelled') AND updated_at < ?",
			cutoff).Scan(&stats.JobsPruned)
	}
	progress("done")

	// 7. Zombie jobs: status='running' but started_at older than threshold.
	progress("Checking for zombie jobs...")
	zombieCutoff := time.Now().Add(-threshold).Format(time.RFC3339)
	_ = db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM jobs WHERE status = 'running' AND started_at <= ?",
		zombieCutoff).Scan(&stats.ZombiesFound)
	if stats.ZombiesFound > 0 && !dryRun {
		now := time.Now().Format(time.RFC3339)
		res, err := db.ExecContext(ctx,
			"UPDATE jobs SET status = 'pending', started_at = NULL, updated_at = ? WHERE status = 'running' AND started_at <= ?",
			now, zombieCutoff)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: reset zombies: %v\n", err)
		} else {
			stats.ZombiesReset, _ = res.RowsAffected()
		}
	}
	progress("done")

	// A dry run answers with the plan it would have carried out, not
	// with the counters a real pass reports. The two are different
	// documents on purpose: a summary says what happened, and nothing
	// in "jobs_pruned: 0" distinguishes a pass that pruned nothing
	// from a projection that pruned nothing because it was never
	// going to. The plan states dry_run outright and names each step.
	if dryRun {
		return renderPreviewPlan(cmd, stats.plan(cmd.CommandPath(), threshold), func(w *lineWriter) {
			stats.print(w)
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), stats.summary())
	}
	summary := &lineWriter{w: cmd.OutOrStdout()}
	stats.print(summary)
	return summary.err
}
