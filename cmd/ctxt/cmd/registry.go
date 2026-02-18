package cmd

import (
	"context"
	"fmt"
	"os"

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

func init() {
	rootCmd.AddCommand(registryCmd)

	// Add subcommands
	registryCmd.AddCommand(registryListCmd)
	registryCmd.AddCommand(registryAddCmd)
	registryCmd.AddCommand(registryRemoveCmd)
	registryCmd.AddCommand(registryInfoCmd)
	registryCmd.AddCommand(registrySyncCmd)
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
	_ = name

	fmt.Println("Registry removal not yet implemented.")
	fmt.Println("Remove registry configuration manually:")
	fmt.Println("  ctxt config edit")
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
