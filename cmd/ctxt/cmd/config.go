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
	Short: "Validate configuration",
	RunE:  runConfigValidate,
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
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	// TODO: Implement actual config show logic
	fmt.Println("Configuration:")
	fmt.Println()
	fmt.Println("storage:")
	fmt.Println("  type: sqlite")
	fmt.Printf("  path: %s\n", cfg.Storage.Path)
	fmt.Println()
	fmt.Println("server:")
	fmt.Printf("  port: %d\n", cfg.Server.Port)
	fmt.Printf("  grpc_port: %d\n", cfg.Server.GRPCPort)
	fmt.Printf("  workers: %d\n", cfg.Server.Workers)
	fmt.Println()
	fmt.Println("profile:")
	fmt.Printf("  default: %s\n", cfg.Profile.Default)

	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	configPath := config.GetConfigPath()
	fmt.Println(configPath)
	return nil
}

func runConfigValidate(cmd *cobra.Command, args []string) error {
	// TODO: Implement actual validation logic
	fmt.Println("Validating configuration...")

	_, err := config.Load(cfgFile)
	if err != nil {
		fmt.Printf("✗ Configuration is invalid: %v\n", err)
		return err
	}

	fmt.Println("✓ Configuration is valid")
	return nil
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
