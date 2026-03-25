package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Create a backup archive of user data",
	Long: `Create a timestamped .tar.gz archive containing:
  - SQLite database snapshot (always)
  - Blob files (with --include-blobs, local backend only)

Output directory defaults to backup.dir in config, then current directory.

Examples:
  dpkms backup
  dpkms backup --include-blobs
  dpkms backup --output /var/backups/ctxt
  dpkms backup --async`,
	RunE: runBackup,
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().Bool("include-blobs", false, "include local blob files in archive")
	backupCmd.Flags().String("output", "", "override output directory path")
	backupCmd.Flags().Bool("async", false, "enqueue as background job and exit")
	backupCmd.Flags().Bool("skip-blob-errors", false, "skip unreadable blobs instead of failing")
}

func runBackup(cmd *cobra.Command, _ []string) error {
	includeBlobs, _ := cmd.Flags().GetBool("include-blobs")
	outputFlag, _ := cmd.Flags().GetString("output")
	async, _ := cmd.Flags().GetBool("async")
	skipBlobErrors, _ := cmd.Flags().GetBool("skip-blob-errors")

	if cfg.Storage.Type != "" && cfg.Storage.Type != "sqlite" {
		return fmt.Errorf("backup only supports sqlite storage (got %s)", cfg.Storage.Type)
	}

	// DB path from config.
	dbPath := cfg.Storage.Path
	if dbPath == "" {
		dbPath = "dpkms.db"
	}

	// Resolve output dir: flag > config > cwd.
	outDir := cfg.Backup.Dir
	if outputFlag != "" {
		outDir = outputFlag
	}

	// Async: enqueue job and exit immediately.
	if async {
		driver, cleanup, err := openDB()
		if err != nil {
			return err
		}
		defer cleanup()

		q := jobs.NewQueue(driver.Jobs())
		now := time.Now().Truncate(time.Second)
		job := &storage.Job{
			ID:       uuid.New().String(),
			Type:     "maintenance",
			Pipeline: "backup",
			Status:   storage.JobPending,
			Payload: fmt.Sprintf(
				`{"include_blobs":%v,"output_dir":%q,"skip_blob_errors":%v}`,
				includeBlobs, outDir, skipBlobErrors,
			),
			Source:    "cli",
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := q.Enqueue(context.Background(), job); err != nil {
			return fmt.Errorf("enqueue backup job: %w", err)
		}
		fmt.Printf("Backup job enqueued: %s\n", job.ID)
		fmt.Printf("Monitor with: dpkms jobs show %s\n", job.ID)
		return nil
	}

	// Sync: run directly.
	opts := service.BackupOpts{
		DBPath:         dbPath,
		BlobCfg:        cfg.Storage.Blob,
		IncludeBlobs:   includeBlobs,
		OutputDir:      outDir,
		SkipBlobErrors: skipBlobErrors,
	}

	fmt.Fprintf(os.Stderr, "-> snapshot db...\n")
	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	fmt.Printf("OK %s (db: %s, blobs: %d, %.1fs)\n",
		result.Path,
		humanBytes(result.DBSize),
		result.BlobCount,
		result.Duration.Seconds(),
	)
	return nil
}

func humanBytes(b int64) string {
	const mb = 1024 * 1024
	if b < mb {
		return fmt.Sprintf("%d KB", b/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/mb)
}
