package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/apierror"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage registries",
	Long: `Manage registry subscriptions and metadata.

Registries provide shared taxonomies, entities, and knowledge objects
for federated search and enrichment.

Examples:
  # List all registries
  ctxt registry list

  # Add a new registry
  ctxt registry add uxpatterns https://uxpatterns.example.com

  # Show registry information
  ctxt registry info uxpatterns

  # Sync registry metadata
  ctxt registry sync uxpatterns

  # Remove a registry
  ctxt registry remove uxpatterns`,
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registries",
	RunE:  runRegistryList,
}

var registryAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a new registry",
	Args:  cobra.ExactArgs(2),
	RunE:  runRegistryAdd,
}

var registryRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryRemove,
}

var registryInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show registry information",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryInfo,
}

var registrySyncCmd = &cobra.Command{
	Use:   "sync <name>",
	Short: "Sync registry metadata",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistrySync,
}

var registrySubmitCmd = &cobra.Command{
	Use:   "submit <bundle-path>",
	Short: "Submit a bundle to the community registry (prints PR instructions)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistrySubmit,
}

func init() {
	rootCmd.AddCommand(registryCmd)

	// Add subcommands
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryRemoveCmd)
	registryCmd.AddCommand(registryInfoCmd)
	registryCmd.AddCommand(registrySyncCmd)
	registryCmd.AddCommand(registrySubmitCmd)
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	registries, total, err := svc.ListRegistries(ctx)
	if err != nil {
		return fmt.Errorf("list registries: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{"registries": registries, "total": total})
	}

	fmt.Printf("Registries (%d)\n\n", total)
	if total == 0 {
		fmt.Println("  No registries configured.")
		fmt.Println("  Add one with: ctxt registry add <name> <url>")
		return nil
	}
	headers := []string{"URL", "Last Fetched"}
	var rows [][]string
	for _, r := range registries {
		rows = append(rows, []string{
			r.RegistryURL,
			r.LastFetched.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := args[1]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	fmt.Printf("Adding registry %s (%s)...\n", name, url)
	if err := svc.FetchRegistry(ctx, url); err != nil {
		return fmt.Errorf("fetch registry: %w", err)
	}
	fmt.Println("Registry added and metadata cached")
	return nil
}

func runRegistryRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	var registryURL string
	for _, r := range cfg.Registries {
		if r.Name == name {
			registryURL = r.URL
			break
		}
	}
	if registryURL == "" {
		return fmt.Errorf("registry %q not found in config", name)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := svc.RemoveRegistry(ctx, registryURL); err != nil {
		return fmt.Errorf("remove registry cache: %w", err)
	}

	// Remove from config
	updated := cfg.Registries[:0]
	for _, r := range cfg.Registries {
		if r.Name != name {
			updated = append(updated, r)
		}
	}
	cfg.Registries = updated

	if err := config.WriteBack(cfg, configPath()); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	fmt.Printf("Removed registry: %s\n", name)
	return nil
}

func runRegistryInfo(cmd *cobra.Command, args []string) error {
	name := args[0]

	var registryURL string
	for _, r := range cfg.Registries {
		if r.Name == name {
			registryURL = r.URL
			break
		}
	}
	if registryURL == "" {
		return fmt.Errorf("registry %q not found in config", name)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	cache, err := svc.Store.Registries().GetCachedManifest(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("get registry cache: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, cache)
	}

	fmt.Printf("Registry: %s\n\n", name)
	fmt.Printf("URL:          %s\n", cache.RegistryURL)
	fmt.Printf("Last Fetched: %s\n", cache.LastFetched.Format("2006-01-02 15:04:05"))
	if cache.ETag != "" {
		fmt.Printf("ETag:         %s\n", cache.ETag)
	}
	if cache.Manifest != nil {
		fmt.Printf("Name:         %s\n", cache.Manifest.Name)
		fmt.Printf("Version:      %s\n", cache.Manifest.Version)
		fmt.Printf("Description:  %s\n", cache.Manifest.Description)
		fmt.Printf("Steps:        %d\n", len(cache.Manifest.Steps))
	}
	return nil
}

func runRegistrySync(cmd *cobra.Command, args []string) error {
	name := args[0]

	var registryURL string
	for _, r := range cfg.Registries {
		if r.Name == name {
			registryURL = r.URL
			break
		}
	}
	if registryURL == "" {
		return fmt.Errorf("registry %q not found in config", name)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	fmt.Printf("Syncing registry %s...\n", name)
	if err := svc.UpdateRegistry(ctx, registryURL); err != nil {
		return fmt.Errorf("sync registry: %w", err)
	}
	fmt.Println("Registry synced")
	return nil
}

// runRegistrySubmit validates a local registry bundle and prints PR instructions.
// No network call is made; this is a guided stub for community submission.
func runRegistrySubmit(cmd *cobra.Command, args []string) error {
	bundlePath := args[0]

	info, err := os.Stat(bundlePath)
	if err != nil {
		if os.IsNotExist(err) {
			apiErr := apierror.New(apierror.CodeRegistryBundleMissing,
				"bundle path does not exist: "+bundlePath, err)
			if isJSONOutput() {
				return outputJSON(cmd.OutOrStdout(), apiErr.JSONBody())
			}
			return apiErr
		}
		return fmt.Errorf("stat bundle: %w", err)
	}

	if info.IsDir() {
		// Expect at least a manifest.json inside.
		manifestPath := bundlePath + "/manifest.json"
		if _, serr := os.Stat(manifestPath); serr != nil {
			apiErr := apierror.New(apierror.CodeRegistryInvalidBundle,
				"bundle directory missing manifest.json", serr)
			if isJSONOutput() {
				return outputJSON(cmd.OutOrStdout(), apiErr.JSONBody())
			}
			return apiErr
		}
	}

	result := map[string]any{
		"bundle":      bundlePath,
		"valid":       true,
		"next_steps":  "Open a pull request at https://github.com/ideacrafterslabs/registry with your bundle.",
		"pr_template": "https://github.com/ideacrafterslabs/registry/blob/main/CONTRIBUTING.md",
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), result)
	}

	fmt.Printf("Bundle validated: %s\n\n", bundlePath)
	fmt.Println("To submit to the community registry, open a PR at:")
	fmt.Println("  https://github.com/ideacrafterslabs/registry")
	fmt.Println()
	fmt.Println("See CONTRIBUTING.md for bundle format requirements:")
	fmt.Println("  https://github.com/ideacrafterslabs/registry/blob/main/CONTRIBUTING.md")
	return nil
}
