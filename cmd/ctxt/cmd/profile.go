package cmd

import (
	"fmt"
	"os"
	"sort"

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
  ctxt profile create myproject

  # Set default profile
  ctxt profile default founder`,
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
	Use:   "default [name]",
	Short: "Set default profile",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runProfileSetDefault,
}

func init() {
	rootCmd.AddCommand(profileCmd)

	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileShowCmd)
	profileCmd.AddCommand(profileCreateCmd)
	profileCmd.AddCommand(profileDeleteCmd)
	profileCmd.AddCommand(profileSetDefaultCmd)
}

func configPath() string {
	if cfgFile != "" {
		return cfgFile
	}
	return config.GetConfigPath()
}

func runProfileList(cmd *cobra.Command, args []string) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"default":  cfg.Profile.Default,
			"profiles": cfg.Profile.Profiles,
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

	if len(cfg.Profile.Profiles) == 0 {
		fmt.Println("  No profiles defined.")
	} else {
		names := make([]string, 0, len(cfg.Profile.Profiles))
		for name := range cfg.Profile.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			p := cfg.Profile.Profiles[name]
			prefix := "  "
			if name == cfg.Profile.Default {
				prefix = "* "
			}
			fmt.Printf("%s%s", prefix, name)
			if p.Description != "" {
				fmt.Printf(" - %s", p.Description)
			}
			fmt.Println()
		}
	}

	fmt.Println()
	fmt.Println("  Configure profiles in your config file:")
	fmt.Printf("  %s\n", config.GetConfigPath())
	return nil
}

func runProfileShow(cmd *cobra.Command, args []string) error {
	name := args[0]
	p, ok := cfg.Profile.Profiles[name]
	if !ok {
		return fmt.Errorf("profile not found: %s", name)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"name":       name,
			"is_default": cfg.Profile.Default == name,
			"profile":    p,
		})
	}

	fmt.Printf("Profile: %s\n", name)
	if cfg.Profile.Default == name {
		fmt.Println("  (default profile)")
	}
	if p.Description != "" {
		fmt.Printf("  Description: %s\n", p.Description)
	}
	if len(p.Tags) > 0 {
		fmt.Printf("  Tags: %v\n", p.Tags)
	}
	if len(p.MentionNamespaces) > 0 {
		fmt.Printf("  Mention Namespaces: %v\n", p.MentionNamespaces)
	}
	if len(p.RerankBoosts) > 0 {
		fmt.Println("  Rerank Boosts:")
		keys := make([]string, 0, len(p.RerankBoosts))
		for k := range p.RerankBoosts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("    %s: %.2f\n", k, p.RerankBoosts[k])
		}
	}
	return nil
}

func runProfileCreate(cmd *cobra.Command, args []string) error {
	name := args[0]

	if cfg.Profile.Profiles == nil {
		cfg.Profile.Profiles = make(map[string]config.FocusProfile)
	}

	if _, exists := cfg.Profile.Profiles[name]; exists {
		return fmt.Errorf("profile already exists: %s", name)
	}

	cfg.Profile.Profiles[name] = config.FocusProfile{
		Description: fmt.Sprintf("Profile for %s", name),
	}

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	fmt.Printf("Created profile: %s\n", name)
	return nil
}

func runProfileDelete(cmd *cobra.Command, args []string) error {
	name := args[0]

	if _, exists := cfg.Profile.Profiles[name]; !exists {
		return fmt.Errorf("profile not found: %s", name)
	}

	delete(cfg.Profile.Profiles, name)
	if cfg.Profile.Default == name {
		cfg.Profile.Default = ""
	}

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	fmt.Printf("Deleted profile: %s\n", name)
	return nil
}

func runProfileSetDefault(cmd *cobra.Command, args []string) error {
	var name string
	if len(args) > 0 {
		name = args[0]
	}

	if name != "" {
		if _, exists := cfg.Profile.Profiles[name]; !exists {
			return fmt.Errorf("profile not found: %s", name)
		}
	}

	cfg.Profile.Default = name

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to set default profile: %w", err)
	}

	if name == "" {
		fmt.Println("Cleared default profile")
	} else {
		fmt.Printf("Set default profile to: %s\n", name)
	}
	return nil
}
