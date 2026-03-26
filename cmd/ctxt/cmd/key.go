package cmd

import (
	"fmt"
	"path/filepath"

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
  ctxt key init`,
}

var keyInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Generate a new Ed25519 signing keypair",
	Long: `Generate a new Ed25519 signing keypair for signed bundle operations.

The private key is stored in the OS keychain under service "ctxt-signing".
The public key is written to:

  ~/.config/contexthelp/keys/<fingerprint>.pub

Use "ctxt config backup" to produce a signed config bundle.
Use "ctxt config restore --verify" to verify before restoring.`,
	RunE: runKeyInit,
}

func init() {
	rootCmd.AddCommand(keyCmd)
	keyCmd.AddCommand(keyInitCmd)
	keyInitCmd.Flags().Bool("force", false, "overwrite existing keypair without prompting")
}

func runKeyInit(cmd *cobra.Command, _ []string) error {
	configPath := config.GetConfigPath()
	configDir := filepath.Dir(configPath)

	kp, err := bundle.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("key init: %w", err)
	}

	// Store private key in OS keychain.
	if err := bundle.StorePrivateKey(kp.Private); err != nil {
		return fmt.Errorf("key init: store private key: %w", err)
	}

	// Save public key file to config dir.
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
	fmt.Fprintf(cmd.OutOrStdout(), "\nRun 'ctxt config backup' to produce a signed bundle.\n")
	return nil
}
