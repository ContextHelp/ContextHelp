package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration operations",
	Long: `Manage ContextHelp configuration.

Examples:
  # Show current configuration
  ctxt config show

  # Show configuration file path
  ctxt config path

  # Validate configuration
  ctxt config validate

  # Edit configuration in default editor
  ctxt config edit`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	RunE:  runConfigShow,
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show configuration file path",
	RunE:  runConfigPath,
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration and optionally scan for plaintext secrets",
	Long: `Validate configuration and optionally scan for plaintext secrets.

Checks that the configuration file is well-formed, then scans string fields for
values that look like plaintext API keys or passwords.

Exit code 1 when validation errors or secret warnings are found.`,
	RunE: runConfigValidate,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit configuration in default editor",
	RunE:  runConfigEdit,
}

func init() {
	rootCmd.AddCommand(configCmd)

	// Add subcommands
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configValidateCmd)
	configCmd.AddCommand(configEditCmd)

	// Flags for validate subcommand
	configValidateCmd.Flags().Bool("check-secrets", true,
		"scan config fields for plaintext secrets and warn")
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, cfg)
	}

	fmt.Println("Configuration:")
	fmt.Println()
	fmt.Printf("  Config file:  %s\n", config.GetConfigPath())
	fmt.Println()
	fmt.Println("  storage:")
	fmt.Printf("    type:       %s\n", cfg.Storage.Type)
	fmt.Printf("    path:       %s\n", cfg.Storage.Path)
	fmt.Println()
	fmt.Println("  server:")
	fmt.Printf("    port:       %d\n", cfg.Server.Port)
	fmt.Printf("    grpc_port:  %d\n", cfg.Server.GRPCPort)
	fmt.Printf("    workers:    %d\n", cfg.Server.Workers)
	fmt.Println()
	fmt.Println("  profile:")
	fmt.Printf("    default:    %s\n", cfg.Profile.Default)
	fmt.Println()
	if len(cfg.Registries) > 0 {
		fmt.Println("  registries:")
		for _, r := range cfg.Registries {
			fmt.Printf("    - %s (%s)\n", r.Name, r.URL)
		}
		fmt.Println()
	}
	if cfg.I18n.Enabled {
		fmt.Println("  i18n:")
		fmt.Printf("    enabled:    true\n")
		fmt.Printf("    languages:  %v\n", cfg.I18n.PreferredLanguages)
	}

	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	configPath := config.GetConfigPath()
	fmt.Println(configPath)
	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	fmt.Println("Validating configuration...")

	// Re-load to get a fresh validation result (cfg may have been loaded early).
	// If load fails, report error; otherwise use cfg (already loaded by initConfig)
	// to avoid the migration write-back corrupting a temporary test file.
	if _, err := config.Load(cfgFile); err != nil {
		fmt.Printf("  ✗ Configuration is invalid: %v\n", err)
		return err
	}

	fmt.Println("  ✓ Configuration is valid")

	checkSecrets, _ := cmd.Flags().GetBool("check-secrets")
	if !checkSecrets {
		return nil
	}

	// Use cfg (loaded by initConfig before migration write-back can affect the
	// file) rather than re-loading, which risks seeing the post-migration YAML
	// that may have field-name differences due to missing yaml struct tags.
	warnings := config.ScanSecrets(cfg)
	if len(warnings) == 0 {
		fmt.Println("  ✓ No plaintext secrets detected")
		return nil
	}

	fmt.Printf("  ✗ %d plaintext secret(s) detected:\n", len(warnings))
	for _, w := range warnings {
		fmt.Printf("      field: %s  value: %s  reason: %s\n", w.Field, w.Hint, w.Reason)
	}
	return fmt.Errorf("plaintext secrets found in configuration")
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	configPath := config.GetConfigPath()

	// Get editor from environment or use default
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	// Open editor
	editorCmd := exec.Command(editor, configPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	fmt.Printf("Opening %s in %s...\n", configPath, editor)
	return editorCmd.Run()
}
