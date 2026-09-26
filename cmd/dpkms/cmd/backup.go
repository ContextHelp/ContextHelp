package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
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
  - Config files and keys (with --include-configs)

Output directory defaults to backup.dir in config, then current directory.
The directory is auto-created if it does not exist, but must be empty.

Examples:
  dpkms backup
  dpkms backup --include-blobs
  dpkms backup --include-configs
  dpkms backup --output-dir /var/backups/ctxt
  dpkms backup --async`,
	RunE: runBackup,
}

func init() {
	rootCmd.AddCommand(backupCmd)
	backupCmd.Flags().Bool("include-blobs", false, "include local blob files in archive")
	// --output-dir, not --output: kit/output reserves -o/--output for the
	// output-path flag globally. T-0465 disambiguates the destination
	// directory used by `dpkms backup`.
	backupCmd.Flags().String("output-dir", "", "override output directory path")
	backupCmd.Flags().Bool("async", false, "enqueue as background job and exit")
	backupCmd.Flags().Bool("skip-blob-errors", false, "skip unreadable blobs instead of failing")
	backupCmd.Flags().Bool("include-configs", false, "include config files and keys in archive")

	// Backup only adds a new archive under the output directory; it
	// never mutates or removes existing data. Write, not Destructive.
	cliconv.WithSideEffect(backupCmd, cliconv.SideEffectWrite)
}

func runBackup(cmd *cobra.Command, _ []string) error {
	includeBlobs, _ := cmd.Flags().GetBool("include-blobs")
	includeConfigs, _ := cmd.Flags().GetBool("include-configs")
	outputFlag, _ := cmd.Flags().GetString("output-dir")
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

	if kitcli.IsDryRun(cmd) {
		previewBackup(cmd, backupPreview{
			DBPath:         dbPath,
			OutDir:         outDir,
			IncludeBlobs:   includeBlobs,
			IncludeConfigs: includeConfigs,
			Async:          async,
		})
		return nil
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

	// Resolve config file path for inclusion in archive.
	configPath := resolveConfigPath()
	configDir := filepath.Dir(configPath)

	// Sync: run directly.
	opts := service.BackupOpts{
		DBPath:         dbPath,
		BlobCfg:        cfg.Storage.Blob,
		IncludeBlobs:   includeBlobs,
		OutputDir:      outDir,
		SkipBlobErrors: skipBlobErrors,
		ConfigPath:     configPath,
		IncludeConfigs: includeConfigs,
		ConfigDir:      configDir,
		EmbeddingInfo:  backupEmbeddingInfo(),
	}

	fmt.Fprintf(os.Stderr, "-> snapshot db...\n")
	result, err := service.Backup(context.Background(), opts)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"path":        result.Path,
			"db_size":     result.DBSize,
			"blob_count":  result.BlobCount,
			"duration_ms": result.Duration.Milliseconds(),
		})
	}

	fmt.Printf("OK %s (db: %s, blobs: %d, %.1fs)\n",
		result.Path,
		humanBytes(result.DBSize),
		result.BlobCount,
		result.Duration.Seconds(),
	)
	return nil
}

// resolveConfigPath mirrors the lookup order in config.Load: --config flag,
// then CTXT_CONFIG env, then default location.
func resolveConfigPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	if envPath := os.Getenv(config.EnvConfigPath); envPath != "" {
		return envPath
	}
	return config.GetConfigPath(binName)
}

func humanBytes(b int64) string {
	const mb = 1024 * 1024
	if b < mb {
		return fmt.Sprintf("%d KB", b/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/mb)
}

// backupPreview carries the resolved inputs a dry-run reports back.
type backupPreview struct {
	DBPath         string
	OutDir         string
	IncludeBlobs   bool
	IncludeConfigs bool
	Async          bool
}

// previewBackup reports what `dpkms backup` would write, without
// creating the output directory or touching the archive. It stats the
// database and the blob directory read-only so the operator sees the
// approximate archive payload before committing to it.
func previewBackup(cmd *cobra.Command, p backupPreview) {
	w := bufio.NewWriter(cmd.OutOrStdout())
	// Preview output is advisory; a broken stdout pipe must not turn a
	// read-only preview into a command failure. Errors surface on Flush.
	printf := func(format string, a ...any) {
		//nolint:errcheck // advisory preview output; Flush reports real failures
		fmt.Fprintf(w, format, a...)
	}

	printf("[dry-run] no writes will occur\n")

	outDir := p.OutDir
	if outDir == "" {
		outDir = "."
	}
	archive := filepath.Join(outDir,
		fmt.Sprintf("ctxt-backup-%s.tar.gz", time.Now().UTC().Format("2006-01-02T15-04-05Z")))

	if p.Async {
		printf("Would enqueue a background backup job (--async).\n")
	}
	printf("Would create archive: %s\n", archive)

	if info, err := os.Stat(p.DBPath); err == nil {
		printf("  database   : %s (%s)\n", p.DBPath, humanBytes(info.Size()))
	} else {
		printf("  database   : %s (not found)\n", p.DBPath)
	}

	if p.IncludeBlobs {
		count, size := countBlobs(cfg.Storage.Blob.Local.Path)
		printf("  blobs      : %d file(s), %s\n", count, humanBytes(size))
	} else {
		printf("  blobs      : skipped (pass --include-blobs)\n")
	}

	if p.IncludeConfigs {
		printf("  configs    : %s\n", filepath.Dir(resolveConfigPath()))
	} else {
		printf("  configs    : skipped (pass --include-configs)\n")
	}

	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		printf("  output dir : %s (would be created)\n", outDir)
	} else {
		printf("  output dir : %s (exists)\n", outDir)
	}

	//nolint:errcheck // nothing actionable once the preview has been emitted
	w.Flush()
}

// countBlobs walks the local blob directory read-only, returning the
// file count and total bytes. Returns zeroes when the path is empty or
// unreadable; a preview must never fail the command.
func countBlobs(root string) (count int, size int64) {
	if root == "" {
		return 0, 0
	}
	// WalkDir's own error is intentionally dropped: an unreadable blob
	// tree yields a zero count rather than failing a read-only preview.
	//nolint:errcheck // walk errors are non-fatal for a preview
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			//nolint:nilerr // unreadable entries are skipped, not fatal
			return nil
		}
		if info, ierr := d.Info(); ierr == nil {
			count++
			size += info.Size()
		}
		return nil
	})
	return count, size
}
