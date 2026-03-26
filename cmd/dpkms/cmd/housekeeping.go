package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// zombieThreshold: jobs stuck in "running" for longer than this are zombies.
const zombieThreshold = 2 * time.Hour

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

	// Prune flags
	pruneCmd.Flags().String("before", "", "remove data before this date (ISO format)")
	pruneCmd.MarkFlagRequired("before")

	// run flags
	runAllCmd.Flags().Duration("zombie-threshold", zombieThreshold,
		"jobs running longer than this are considered zombies")
	runAllCmd.Flags().Bool("dry-run", false, "report actions without executing them")

	// Bind flags to viper
	viper.BindPFlag("housekeeping.before", pruneCmd.Flags().Lookup("before"))
}

func runVacuum(cmd *cobra.Command, args []string) error {
	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("Running VACUUM on database...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	if _, err := driver.DB().ExecContext(context.Background(), "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	fmt.Println("Vacuum completed")
	return nil
}

func runReindex(cmd *cobra.Command, args []string) error {
	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("Rebuilding search indexes...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	ctx := context.Background()
	db := driver.DB()

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

	fmt.Println("Compacting database...")
	fmt.Printf("Database: %s\n\n", cfg.Storage.Path)

	ctx := context.Background()
	db := driver.DB()

	if _, err := db.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		return fmt.Errorf("optimize: %w", err)
	}
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}

	fmt.Println("Compaction completed")
	return nil
}

func runPrune(cmd *cobra.Command, args []string) error {
	before := viper.GetString("housekeeping.before")
	beforeTime, err := time.Parse("2006-01-02", before)
	if err != nil {
		return fmt.Errorf("invalid date format (expected YYYY-MM-DD): %w", err)
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

func (s housekeepingStats) print() {
	reclaimedKB := (s.PagesBefore - s.PagesAfter) * s.PageSize / 1024
	currentMB := s.PagesAfter * s.PageSize / (1024 * 1024)

	fmt.Println("\n--- Housekeeping Summary ---")
	fmt.Printf("  DB size now    : %d MB\n", currentMB)
	fmt.Printf("  Space reclaimed: %d KB\n", reclaimedKB)
	fmt.Printf("  Jobs pruned    : %d\n", s.JobsPruned)
	fmt.Printf("  Zombie jobs    : %d found, %d reset to pending\n", s.ZombiesFound, s.ZombiesReset)
	fmt.Printf("  Orphan blobs   : %d\n", s.OrphanBlobs)
	if s.EmbeddingsOpt {
		fmt.Println("  Embeddings     : optimized")
	} else {
		fmt.Println("  Embeddings     : skipped (table absent)")
	}
	fmt.Println("----------------------------")
}

func runAll(cmd *cobra.Command, _ []string) error {
	threshold, _ := cmd.Flags().GetDuration("zombie-threshold")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	driver, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	db := driver.DB()
	stats := housekeepingStats{}

	if dryRun {
		fmt.Println("[dry-run] no writes will occur")
	}

	// 1. Page count before.
	_ = db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PagesBefore)
	_ = db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&stats.PageSize)

	// 2. WAL checkpoint.
	fmt.Print("Checkpointing WAL... ")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: wal_checkpoint: %v\n", err)
		}
	}
	fmt.Println("done")

	// 3. VACUUM.
	fmt.Print("Running VACUUM... ")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
			return fmt.Errorf("vacuum: %w", err)
		}
	}
	fmt.Println("done")

	// Page count after vacuum.
	_ = db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&stats.PagesAfter)

	// 4. Compact / optimize embeddings table (best-effort).
	fmt.Print("Optimizing embeddings... ")
	if !dryRun {
		if _, err := db.ExecContext(ctx, "INSERT INTO embeddings_fts(embeddings_fts) VALUES('optimize')"); err == nil {
			stats.EmbeddingsOpt = true
		}
	}
	fmt.Println("done")

	// 5. Orphaned blobs: blobs whose object_id references no known object.
	fmt.Print("Purging orphaned blobs... ")
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
	fmt.Println("done")

	// 6. Prune jobs history >30 days.
	fmt.Print("Pruning old jobs... ")
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
	fmt.Println("done")

	// 7. Zombie jobs: status='running' but started_at older than threshold.
	fmt.Print("Checking for zombie jobs... ")
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
	fmt.Println("done")

	stats.print()
	return nil
}
