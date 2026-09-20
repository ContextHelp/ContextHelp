package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

var restoreCmd = &cobra.Command{
	Use:   "restore <archive.tar.gz | directory>",
	Short: "Restore from a backup archive or directory",
	Long: `Restore a ctxt backup from a .tar.gz archive or an extracted directory.

Restores:
  - SQLite database
  - Config files + keys (if present in backup)
  - Blob files (if present in backup)

The target data directory is taken from config (storage.path) unless
overridden with --data-dir. Existing files are overwritten.

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

	configPath := resolveConfigPath()

	opts := service.RestoreOpts{
		Source:      source,
		DBPath:      dbPath,
		ConfigDir:   configPath,
		SkipConfigs: skipConfigs,
		DryRun:      dryRun,
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
