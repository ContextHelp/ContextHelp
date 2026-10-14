package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <archive.tar.gz | directory>",
	Short: "Restore from a backup archive or directory",
	Long: `Restore a ctxt backup from a .tar.gz archive or an extracted directory.

Restores:
  - SQLite database
  - Config files + keys (if present in backup)
  - Blobs (if present in backup), into the storage.blob store

The database is restored to storage.path, which defaults to db.sqlite in
the data directory (--data-dir, CTXT_DATA_DIR or storage.data_dir), so
--data-dir moves the default target. Existing files are overwritten.

Examples:
  dpkms restore ctxt-backup-2026-04-25T13-51-54Z.tar.gz
  dpkms restore /path/to/extracted/backup/
  dpkms restore backup.tar.gz --data-dir /new/location`,
	Args: cobra.ExactArgs(1),
	RunE: runRestore,
}

func init() {
	rootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().Bool("skip-configs", false, "skip restoring config files")
	// --dry-run is NOT declared here: kit owns it as a root persistent
	// flag. The former local declaration shadowed the inherited one and
	// left the kit.dry_run viper binding unset. Read via kitcli.IsDryRun.

	// Restore overwrites the live database, config and blobs in place
	// with whatever the archive holds; there is no undo short of
	// another backup. Destructive-local because the blast radius is
	// this instance's data directory, and typed-token because an
	// operator who restores the wrong archive loses everything since
	// it was taken.
	cliconv.WithSideEffect(restoreCmd, cliconv.SideEffectDestructiveLocal)
	cliconv.WithDestructiveToken(restoreCmd)
}

func runRestore(cmd *cobra.Command, args []string) error {
	source := args[0]
	skipConfigs, _ := cmd.Flags().GetBool("skip-configs")
	dryRun := kitcli.IsDryRun(cmd)

	dbPath := cfg.Storage.Path
	if dbPath == "" {
		dbPath = "dpkms.db"
	}

	opts := service.RestoreOpts{
		Source:      source,
		DBPath:      dbPath,
		ConfigDir:   filepath.Dir(resolveConfigPath()),
		SkipConfigs: skipConfigs,
		DryRun:      dryRun,
	}
	// Blobs go to the configured store (storage.blob), not a path
	// derived here. A dry run writes nothing, so opens none.
	if !dryRun {
		bs, err := blob.New(cfg.Storage.Blob)
		if err != nil {
			return fmt.Errorf("restore: open blob store: %w", err)
		}
		opts.Blobs = bs
	}

	fmt.Fprintf(os.Stderr, "-> restoring from %s...\n", source)
	result, err := service.Restore(cmd.Context(), opts)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	if dryRun {
		fmt.Println("Dry run — no files written.")
	}
	fmt.Printf("OK (db: %s, configs: %d, blobs: %d)\n",
		humanBytes(result.DBSize),
		result.ConfigCount,
		result.BlobCount,
	)
	return nil
}
