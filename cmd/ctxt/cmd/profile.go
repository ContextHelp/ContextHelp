package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage focus profiles",
	Long: `Manage focus profiles for contextual filtering and reranking.

Focus profiles allow you to tailor ContextHelp's behavior to specific
roles or projects (e.g., Founder, Engineer, Research).

Examples:
  # List all profiles
  ctxt profile list

  # Show profile details
  ctxt profile show founder

  # Create a new profile
  ctxt profile create myproject --config profile.yaml

  # Set default profile
  ctxt profile set-default founder`,
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all profiles",
	RunE:  runProfileList,
}

var profileShowCmd = &cobra.Command{
	Use:   "show <name>",
	Short: "Show profile details",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileShow,
}

var profileCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileCreate,
}

var profileDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileDelete,
}

var profileSetDefaultCmd = &cobra.Command{
	Use:   "set-default <name>",
	Short: "Set default profile",
	Args:  cobra.ExactArgs(1),
	RunE:  runProfileSetDefault,
}

func init() {
	rootCmd.AddCommand(profileCmd)

	// Add subcommands
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileDeleteCmd)
	profileCmd.AddCommand(profileSetDefaultCmd)

	// Create flags
	profileCreateCmd.Flags().String("config", "", "path to profile configuration file")
}

func runProfileList(cmd *cobra.Command, args []string) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"default": cfg.Profile.Default,
		})
	}

	fmt.Println("Focus Profiles:")
	fmt.Println()
	defaultProfile := cfg.Profile.Default
	if defaultProfile == "" {
		defaultProfile = "(none)"
	}
	fmt.Printf("  Default profile: %s\n", defaultProfile)
	fmt.Println()
	fmt.Println("  Configure profiles in your config file:")
	fmt.Printf("  %s\n", config.GetConfigPath())
	return nil
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	name := args[0]

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"name":       name,
			"is_default": cfg.Profile.Default == name,
		})
	}

	fmt.Printf("Profile: %s\n\n", name)
	if cfg.Profile.Default == name {
		fmt.Println("  (default profile)")
	}
	fmt.Println()
	fmt.Println("  Profile details are stored in config file.")
	fmt.Println("  Edit: ctxt config edit")
	return nil
}

func runProfileCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	fmt.Printf("Creating profile: %s\n", name)
	fmt.Println()
	fmt.Println("Profile storage not yet implemented.")
	fmt.Println("Add profile configuration manually:")
	fmt.Println("  ctxt config edit")
	return nil
}

func runProfileDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	fmt.Printf("Deleting profile: %s\n", name)
	fmt.Println()
	fmt.Println("Profile storage not yet implemented.")
	fmt.Println("Remove profile configuration manually:")
	fmt.Println("  ctxt config edit")
	return nil
}

func runProfileSetDefault(cmd *cobra.Command, args []string) error {
	name := args[0]
	fmt.Printf("Setting default profile to: %s\n", name)
	fmt.Println()
	fmt.Println("Profile storage not yet implemented.")
	fmt.Println("Set default profile manually in config:")
	fmt.Println("  ctxt config edit")
	return nil
}
