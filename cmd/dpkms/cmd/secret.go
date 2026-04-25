package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage secrets",
	Long: `Read and write secrets from the configured backend.

The active backend is set via secrets.backend in your config file.
Supported backends: env, keychain, age-file, 1password, gh-secrets.

Examples:
  # Get a secret
  dpkms secret get OPENAI_API_KEY

  # Set a secret (backend must support writes)
  dpkms secret set OPENAI_API_KEY sk-...

  # Show active backend config
  dpkms secret list

Note: passing secrets as command-line arguments exposes them in shell
history. For sensitive values prefer setting them via your backend's
native tooling (e.g. security add-generic-password for keychain).`,
}

var secretGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a secret value",
	Args:  cobra.ExactArgs(1),
	RunE:  runSecretGet,
}

var secretSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a secret value",
	Args:  cobra.ExactArgs(2),
	RunE:  runSecretSet,
}

var secretListCmd = &cobra.Command{
	Use:   "list",
	Short: "List secrets (or show backend config if enumeration not supported)",
	RunE:  runSecretList,
}

func init() {
	rootCmd.AddCommand(secretCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretListCmd)
}

func runSecretGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	r, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret get: %w", err)
	}

	value, err := r.Get(key)
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]string{"key": key, "value": value})
	}
	fmt.Fprintln(cmd.OutOrStdout(), value)
	return nil
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	key, value := args[0], args[1]

	r, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret set: %w", err)
	}

	if err := r.Set(key, value); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Set %s\n", key)
	return nil
}

func runSecretList(cmd *cobra.Command, _ []string) error {
	backend := cfg.Secrets.Backend
	if backend == "" {
		backend = "env"
	}

	r, err := secrets.NewResolver(cfg.Secrets)
	if err != nil {
		return fmt.Errorf("secret list: %w", err)
	}

	if lister, ok := r.(secrets.Lister); ok {
		keys, err := lister.Keys()
		if err != nil {
			return fmt.Errorf("secret list: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(os.Stdout, map[string]any{"backend": backend, "keys": keys})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Secrets backend: %s\n", backend)
		if len(keys) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "  (no secrets stored)")
		} else {
			for _, k := range keys {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", k)
			}
		}
		return nil
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"backend":           backend,
			"enumerable":        false,
			"keychain_service":  cfg.Secrets.KeychainService,
			"age_file":          cfg.Secrets.AgeFile,
			"age_identity_file": cfg.Secrets.AgeIdentityFile,
			"onepassword_vault": cfg.Secrets.OnePasswordVault,
			"gh_repo":           cfg.Secrets.GHRepo,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Secrets backend: %s\n", backend)
	switch backend {
	case "env":
		fmt.Fprintln(cmd.OutOrStdout(),
			"  Key enumeration not supported — set env vars manually.")
	case "keychain":
		svc := cfg.Secrets.KeychainService
		if svc == "" {
			svc = "ctxt"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "  service: %s\n", svc)
	case "1password":
		fmt.Fprintf(cmd.OutOrStdout(), "  vault: %s\n", cfg.Secrets.OnePasswordVault)
		fmt.Fprintln(cmd.OutOrStdout(),
			"  Key enumeration not supported — use: op item list --vault <vault>")
	}
	return nil
}
