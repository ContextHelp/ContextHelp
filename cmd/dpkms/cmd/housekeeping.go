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

func init() {
	rootCmd.AddCommand(housekeepingCmd)

	// Add subcommands
	housekeepingCmd.AddCommand(vacuumCmd)
	housekeepingCmd.AddCommand(reindexCmd)
	housekeepingCmd.AddCommand(compactCmd)
	housekeepingCmd.AddCommand(pruneCmd)

	// Prune flags
	pruneCmd.Flags().String("before", "", "remove data before this date (ISO format)")
	pruneCmd.MarkFlagRequired("before")

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
