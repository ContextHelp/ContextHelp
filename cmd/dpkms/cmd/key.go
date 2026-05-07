package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/bundle"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{
	Use:   "key",
	Short: "Manage signing keys",
	Long: `Manage Ed25519 signing keys for signed config bundle export/import.

Examples:
  # Generate a new Ed25519 keypair
  dpkms key init

  # Rotate the active signing key
  dpkms key rotate`,
}

var keyInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a new Ed25519 signing keypair",
	Long: `Generate a new Ed25519 signing keypair for signed bundle operations.

The private key is stored in the OS keychain under service "ctxt-signing".
The public key is written to:

  ~/.config/contexthelp/keys/<fingerprint>.pub

Use "dpkms config backup" to produce a signed config bundle.
Use "dpkms config restore --verify" to verify before restoring.`,
	RunE: runKeyInit,
}

var keyRotateCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate the active Ed25519 signing key",
	Long: `Rotate the active Ed25519 signing key.

Steps performed:
  1. Generate a new Ed25519 keypair.
  2. Create a transition record signed by both old and new key.
  3. Append the record to ~/.config/contexthelp/keys/rotation_log.json.
  4. Archive the old private key to signing.key.bak.<timestamp>.
  5. Store the new private key in the OS keychain.
  6. Save the new public key file.

Import verification honours the rotation chain: bundles signed by a previously
valid key are still accepted as long as the chain is intact.

A warning is emitted if the current signing key is older than 365 days.`,
	RunE: runKeyRotate,
}

func init() {
	rootCmd.AddCommand(keyCmd)
	keyCmd.AddCommand(keyInitCmd)
	keyCmd.AddCommand(keyRotateCmd)
	keyInitCmd.Flags().Bool("force", false, "overwrite existing keypair without prompting")
}

func runKeyInit(cmd *cobra.Command, _ []string) error {
	configPath := config.GetConfigPath(binName)
	configDir := filepath.Dir(configPath)

	kp, err := bundle.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("key init: %w", err)
	}

	if err := bundle.StorePrivateKey(kp.Private); err != nil {
		return fmt.Errorf("key init: store private key: %w", err)
	}

	if err := bundle.SavePublicKey(configDir, kp); err != nil {
		return fmt.Errorf("key init: save public key: %w", err)
	}

	pubPath := bundle.PublicKeyPath(configDir, kp.Fingerprint)

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]string{
			"fingerprint": kp.Fingerprint,
			"public_key":  pubPath,
			"keychain":    bundle.KeychainService + "/" + bundle.KeychainAccount,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Ed25519 keypair generated\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  fingerprint:  %s\n", kp.Fingerprint)
	fmt.Fprintf(cmd.OutOrStdout(), "  public key:   %s\n", pubPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  private key:  OS keychain (%s/%s)\n",
		bundle.KeychainService, bundle.KeychainAccount)
	fmt.Fprintf(cmd.OutOrStdout(),
		"\nRun 'dpkms config backup' to produce a signed bundle.\n")
	return nil
}

func runKeyRotate(cmd *cobra.Command, _ []string) error {
	configPath := config.GetConfigPath(binName)
	configDir := filepath.Dir(configPath)
	keysDir := bundle.PublicKeyDir(configDir)

	if age, err := bundle.SigningKeyAge(keysDir); err == nil {
		if age > bundle.KeyAgeDays*24*time.Hour {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: signing.key is %.0f days old (threshold: %d days) — rotation overdue\n",
				age.Hours()/24, bundle.KeyAgeDays)
		}
	}

	oldPriv, err := bundle.LoadPrivateKey()
	if err != nil {
		return fmt.Errorf("key rotate: load current private key: %w", err)
	}

	newKP, res, err := bundle.RotateKeys(keysDir, oldPriv)
	if err != nil {
		return fmt.Errorf("key rotate: %w", err)
	}

	if err := bundle.StorePrivateKey(newKP.Private); err != nil {
		return fmt.Errorf("key rotate: store new private key: %w", err)
	}

	if err := bundle.SavePublicKey(configDir, newKP); err != nil {
		return fmt.Errorf("key rotate: save new public key: %w", err)
	}

	rotLogPath := bundle.RotationLogPath(keysDir)
	newPubPath := bundle.PublicKeyPath(configDir, newKP.Fingerprint)

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]string{
			"old_fingerprint": res.OldFingerprint,
			"new_fingerprint": res.NewFingerprint,
			"archive":         res.OldKeyArchive,
			"rotation_log":    rotLogPath,
			"new_public_key":  newPubPath,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Key rotation complete\n")
	fmt.Fprintf(cmd.OutOrStdout(), "  old fingerprint:  %s\n", res.OldFingerprint)
	fmt.Fprintf(cmd.OutOrStdout(), "  new fingerprint:  %s\n", res.NewFingerprint)
	fmt.Fprintf(cmd.OutOrStdout(), "  old key archived: %s\n", res.OldKeyArchive)
	fmt.Fprintf(cmd.OutOrStdout(), "  rotation log:     %s\n", rotLogPath)
	fmt.Fprintf(cmd.OutOrStdout(), "  new public key:   %s\n", newPubPath)
	fmt.Fprintf(cmd.OutOrStdout(),
		"\nBundles signed by the old key remain verifiable via the rotation chain.\n")
	return nil
}
