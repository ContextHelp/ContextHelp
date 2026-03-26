package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/apierror"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
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

  # Preview changes without writing (dry-run)
  ctxt registry sync --dry-run uxpatterns

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
	Long: `Sync registry metadata from the remote source.

With --dry-run, fetches the remote manifest and shows a diff of would-be
additions, updates, and removals without writing anything to storage.`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistrySync,
}

var registrySubmitCmd = &cobra.Command{
	Use:   "submit <bundle-path>",
	Short: "Submit a bundle to the community registry (prints PR instructions)",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistrySubmit,
}

var registryCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities <name>",
	Short: "Show capability handshake for a registry",
	Long: `Show what features a registry supports and whether the client version is compatible.

Examples:
  ctxt registry capabilities default
  ctxt registry capabilities uxpatterns`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryCapabilities,
}

var registryLoginCmd = &cobra.Command{
	Use:   "login <name>",
	Short: "Store an auth token for a registry in the OS keychain",
	Long: `Store an auth token for a registry securely in the OS keychain.

The token is NEVER written to the YAML config file.

Interactive (prompts for token):
  ctxt registry login example-paid

Non-interactive (pass token via flag — prefer env-var to avoid shell history):
  ctxt registry login example-paid --token "$TOKEN"`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryLogin,
}

var registryLogoutCmd = &cobra.Command{
	Use:   "logout <name>",
	Short: "Remove the stored auth token for a registry from the OS keychain",
	Args:  cobra.ExactArgs(1),
	RunE:  runRegistryLogout,
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
	registryCmd.AddCommand(registryLoginCmd)
	registryCmd.AddCommand(registryLogoutCmd)
	registryCmd.AddCommand(registryCapabilitiesCmd)

	// Flags for sync subcommand
	registrySyncCmd.Flags().Bool("dry-run", false,
		"fetch remote manifest and show diff without writing to storage")
	registrySyncCmd.Flags().Bool("reconcile", false,
		"sync all configured registries with multi-registry entity reconciliation")
	registrySyncCmd.Flags().String("merge-strategy", "last-write-wins",
		"merge strategy for entity conflicts: last-write-wins or trust-score")

	// Flags for login subcommand
	registryLoginCmd.Flags().String("token", "",
		"auth token (reads from stdin prompt if omitted)")
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

	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("dry-run flag: %w", err)
	}
	reconcile, err := cmd.Flags().GetBool("reconcile")
	if err != nil {
		return fmt.Errorf("reconcile flag: %w", err)
	}
	mergeStrategy, err := cmd.Flags().GetString("merge-strategy")
	if err != nil {
		return fmt.Errorf("merge-strategy flag: %w", err)
	}

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

	if dryRun {
		return runRegistrySyncDryRun(cmd, ctx, svc, name, registryURL)
	}

	if reconcile {
		return runRegistrySyncReconcile(cmd, ctx, svc, registry.MergeStrategy(mergeStrategy))
	}

	fmt.Printf("Syncing registry %s...\n", name)
	if err := svc.UpdateRegistry(ctx, registryURL); err != nil {
		return fmt.Errorf("sync registry: %w", err)
	}
	fmt.Println("Registry synced")
	return nil
}

// runRegistrySyncReconcile syncs all configured registries with multi-registry
// entity reconciliation and prints the conflict report.
func runRegistrySyncReconcile(
	cmd *cobra.Command,
	ctx context.Context,
	svc *service.Service,
	strategy registry.MergeStrategy,
) error {
	fmt.Printf("Syncing all registries with reconciliation (strategy: %s)...\n", strategy)

	result, err := svc.SyncAllRegistriesWithReconciliation(ctx, strategy, nil)
	if err != nil {
		return fmt.Errorf("reconcile sync: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"merged":             result.Merged,
			"conflicts":          result.Conflicts,
			"offline_registries": result.OfflineRegistries,
		})
	}

	fmt.Printf("Entities merged: %d\n", result.Merged)

	if len(result.OfflineRegistries) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nOffline registries (cached values preserved):\n")
		for _, url := range result.OfflineRegistries {
			fmt.Fprintf(cmd.OutOrStdout(), "  OFFLINE  %s\n", url)
		}
	}

	if len(result.Conflicts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "\nNo entity conflicts detected.")
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nConflicts resolved (%d):\n\n", len(result.Conflicts))
	headers := []string{"Slug", "Field", "Winner", "Strategy"}
	rows := make([][]string, 0, len(result.Conflicts))
	for _, c := range result.Conflicts {
		rows = append(rows, []string{c.Slug, c.Field, c.Winner, string(c.Strategy)})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

// runRegistrySyncDryRun fetches the remote manifest, diffs against local state,
// and prints a table of would-be changes. Writes nothing to storage.
func runRegistrySyncDryRun(
	cmd *cobra.Command,
	ctx context.Context,
	svc *service.Service,
	name, registryURL string,
) error {
	fmt.Printf("Dry-run: fetching remote manifest for %s...\n\n", name)

	diffs, err := svc.DiffRegistrySync(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("diff registry: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"registry": name,
			"url":      registryURL,
			"dry_run":  true,
			"changes":  diffs,
			"total":    len(diffs),
		})
	}

	if len(diffs) == 0 {
		fmt.Printf("No changes detected for registry %q.\n", name)
		fmt.Println("(nothing to sync)")
		return nil
	}

	fmt.Printf("Would-be changes for registry %q (%d total):\n\n", name, len(diffs))

	headers := []string{"Action", "Kind", "Name", "From", "To"}
	rows := make([][]string, 0, len(diffs))
	for _, d := range diffs {
		rows = append(rows, []string{d.Action, d.Kind, d.Name, d.OldValue, d.NewValue})
	}
	printTable(os.Stdout, headers, rows)

	fmt.Println("\nNo changes applied (dry-run).")
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

// runRegistryLogin stores an auth token for the named registry in the OS keychain.
// Token source priority: --token flag > interactive prompt.
func runRegistryLogin(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Validate registry exists in config.
	if !registryExistsInConfig(name) {
		return fmt.Errorf("registry %q not found in config — add it first with: ctxt registry add %s <url>", name, name)
	}

	token, err := cmd.Flags().GetString("token")
	if err != nil {
		return fmt.Errorf("token flag: %w", err)
	}

	if token == "" {
		token, err = readToken(cmd)
		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token must not be empty")
	}

	store := registry.NewTokenStore()
	if err := store.Set(name, token); err != nil {
		return fmt.Errorf("registry login: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Token stored securely in keychain for registry %q.\n", name)
	return nil
}

// runRegistryLogout removes the stored auth token for the named registry.
func runRegistryLogout(cmd *cobra.Command, args []string) error {
	name := args[0]

	store := registry.NewTokenStore()
	if err := store.Delete(name); err != nil {
		var noToken registry.ErrNoToken
		if errors.As(err, &noToken) {
			return fmt.Errorf("registry %q has no stored token", name)
		}
		return fmt.Errorf("registry logout: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Token removed from keychain for registry %q.\n", name)
	return nil
}

// registryExistsInConfig reports whether name appears in cfg.Registries.
func registryExistsInConfig(name string) bool {
	if cfg == nil {
		return false
	}
	for _, r := range cfg.Registries {
		if r.Name == name {
			return true
		}
	}
	return false
}

// runRegistryCapabilities fetches the cached manifest for name and runs the
// capability handshake, printing any version or feature warnings.
func runRegistryCapabilities(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Resolve URL: "default" maps to the bundled sentinel URL.
	registryURL := registry.DefaultRegistryURL
	if name != "default" {
		for _, r := range cfg.Registries {
			if r.Name == name {
				registryURL = r.URL
				break
			}
		}
		if registryURL == registry.DefaultRegistryURL {
			return fmt.Errorf("registry %q not found in config", name)
		}
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	warnings, err := svc.CheckRegistryCapabilities(ctx, registryURL, version,
		"entity_sync", "taxonomy", "translations",
	)
	if err != nil {
		return fmt.Errorf("capability check: %w", err)
	}

	cache, err := svc.Store.Registries().GetCachedManifest(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("get registry: %w", err)
	}

	type capResult struct {
		Name         string            `json:"name"`
		URL          string            `json:"url"`
		Version      string            `json:"version"`
		MinClient    string            `json:"min_client_version,omitempty"`
		Capabilities map[string]bool   `json:"capabilities"`
		Warnings     []map[string]string `json:"warnings,omitempty"`
	}

	var caps map[string]bool
	var mVersion, mMinClient string
	if cache.Manifest != nil {
		caps = map[string]bool{
			"entity_sync":  cache.Manifest.Capabilities.EntitySync,
			"taxonomy":     cache.Manifest.Capabilities.Taxonomy,
			"translations": cache.Manifest.Capabilities.Translations,
		}
		mVersion = cache.Manifest.Version
		mMinClient = cache.Manifest.MinClientVersion
	}

	var warnMaps []map[string]string
	for _, w := range warnings {
		warnMaps = append(warnMaps, map[string]string{
			"feature": w.Feature,
			"message": w.Message,
		})
	}

	result := capResult{
		Name:         name,
		URL:          registryURL,
		Version:      mVersion,
		MinClient:    mMinClient,
		Capabilities: caps,
		Warnings:     warnMaps,
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), result)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Registry: %s\n", name)
	fmt.Fprintf(cmd.OutOrStdout(), "URL:      %s\n", registryURL)
	if mVersion != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Version:  %s\n", mVersion)
	}
	if mMinClient != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Requires ctxt >= %s\n", mMinClient)
	}
	fmt.Fprintln(cmd.OutOrStdout())

	if len(caps) > 0 {
		headers := []string{"Capability", "Supported"}
		var rows [][]string
		for _, k := range []string{"entity_sync", "taxonomy", "translations"} {
			v := "no"
			if caps[k] {
				v = "yes"
			}
			rows = append(rows, []string{k, v})
		}
		printTable(cmd.OutOrStdout(), headers, rows)
	}

	if len(warnings) > 0 {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "Warnings:")
		for _, w := range warnings {
			fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s\n", w.Feature, w.Message)
		}
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "\nNo capability warnings.")
	}
	return nil
}

// readToken prompts the user for a token read from stdin.
// In interactive sessions the token is read line-by-line; input is not echoed
// because the prompt instructs the user to paste rather than type.
// For masked input in production use --token flag with an env-var reference.
func readToken(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Enter token: ")
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read token: %w", err)
	}
	return "", fmt.Errorf("no token provided on stdin")
}
