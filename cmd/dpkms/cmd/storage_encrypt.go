package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	encryptedblob "github.com/ideacrafterslabs/ctxt/internal/storage/blob/encrypted"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage storage encryption and key rotation",
}

var storageEncryptCmd = &cobra.Command{
	Use:   "encrypt",
	Short: "Encrypt an existing plaintext SQLite database and local blobs at rest",
	Long:  "Migrate an existing plaintext SQLite database (via crash-safe VACUUM INTO) and any plaintext local blobs to encrypted at-rest storage using the configured storage.encryption key provider.",
	RunE:  runStorageEncrypt,
}

var storageRekeyCmd = &cobra.Command{
	Use:   "rekey",
	Short: "Rotate storage encryption keys (KEK rewrap or full SQLite DEK rekey)",
	Long:  "Rotate the key-encryption key wrapping the SQLite database key envelope (default, O(1)) or rekey the database pages with a fresh data-encryption key (--rewrap-only=false).",
	RunE:  runStorageRekey,
}

func init() {
	rootCmd.AddCommand(storageCmd)
	storageCmd.AddCommand(storageEncryptCmd, storageRekeyCmd)
	storageRekeyCmd.Flags().Bool("rewrap-only", true, "rotate KEK envelope only without rewriting SQLite pages")
	storageRekeyCmd.Flags().String("to-key", "", "target KEK id (defaults to provider current key)")
	cliconv.WithSideEffect(storageEncryptCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(storageEncryptCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(storageEncryptCmd, []cliconv.Example{
		{Title: "Encrypt existing SQLite database and blobs", Command: "dpkms storage encrypt"},
	})
	cliconv.WithNextSteps(storageEncryptCmd, []cliconv.NextStep{
		{When: "after migration", Suggest: "dpkms doctor", Reason: "verify encryption health and key provider unlock"},
	})
	cliconv.WithSideEffect(storageRekeyCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(storageRekeyCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(storageRekeyCmd, []cliconv.Example{
		{Title: "Rotate KEK envelope (O(1))", Command: "dpkms storage rekey --to-key k2"},
		{Title: "Full SQLite DEK rekey", Command: "dpkms storage rekey --rewrap-only=false"},
	})
	cliconv.WithNextSteps(storageRekeyCmd, []cliconv.NextStep{
		{When: "after rekeying", Suggest: "dpkms doctor", Reason: "confirm the daemon unlocks cleanly with the rotated key"},
	})
}

func runStorageEncrypt(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()
	if !cfg.Storage.Encryption.Enabled {
		return fmt.Errorf("storage encrypt: storage.encryption.enabled is false in config")
	}
	kp, err := keys.Open(ctx, cfg.Storage.Encryption, cfg.Secrets)
	if err != nil {
		return fmt.Errorf("storage encrypt: unlock key provider: %w", err)
	}
	typ, dbPath := serveStorageTarget(cfg)
	if typ != storageTypeSQLite {
		return fmt.Errorf("storage encrypt: SQLite migration applies only to storage.type=sqlite (got %s)", typ)
	}
	migratedDB, err := sqlite.EncryptDatabase(ctx, dbPath, kp)
	if err != nil {
		return err
	}
	bs, err := blob.New(cfg.Storage.Blob)
	if err != nil {
		return fmt.Errorf("storage encrypt: open blob store: %w", err)
	}
	migratedBlobs, err := encryptedblob.MigratePlaintext(ctx, bs, kp)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Storage encryption complete (db_migrated=%v, blobs_migrated=%d)\n", migratedDB, migratedBlobs)
	return nil
}

func runStorageRekey(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()
	if !cfg.Storage.Encryption.Enabled {
		return fmt.Errorf("storage rekey: storage.encryption.enabled is false in config")
	}
	kp, err := keys.Open(ctx, cfg.Storage.Encryption, cfg.Secrets)
	if err != nil {
		return fmt.Errorf("storage rekey: unlock key provider: %w", err)
	}
	rewrapOnly, _ := cmd.Flags().GetBool("rewrap-only")
	toKey, _ := cmd.Flags().GetString("to-key")
	typ, dbPath := serveStorageTarget(cfg)
	if typ != storageTypeSQLite {
		return fmt.Errorf("storage rekey: SQLite rekey applies only to storage.type=sqlite (got %s)", typ)
	}
	if rewrapOnly {
		usedID, err := sqlite.RewrapDatabaseKey(ctx, dbPath, kp, toKey)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Rewrapped SQLite database key envelope under KEK %s\n", usedID)
		return nil
	}
	if err := sqlite.RekeyDatabase(ctx, dbPath, kp); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Rekeyed SQLite database %s with fresh DEK\n", dbPath)
	return nil
}
