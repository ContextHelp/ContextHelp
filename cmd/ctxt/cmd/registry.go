package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/apierror"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "Manage registries",
	Long: `Manage registry subscriptions and metadata.

Registries provide shared taxonomies, entities, and knowledge objects
for federated search and enrichment.

Examples:
  # List cached registry manifests
  ctxt registry list

  # Cache a registry's manifest (subscribe in the config file's registries list)
  ctxt registry add uxpatterns https://uxpatterns.example.com

  # Show registry information
  ctxt registry info uxpatterns

  # Sync registry metadata
  ctxt registry sync uxpatterns

  # Preview changes without writing (dry-run)
  ctxt registry sync --dry-run uxpatterns

  # Remove a registry
  ctxt registry delete uxpatterns --confirm-token=<sha>`,
}

var registryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cached registry manifests",
	Long: `List the registry manifests in the local cache (what add and sync
fetched), with each URL and its last fetch timestamp. Read-only.

The config file's registries list is a separate set: a cached manifest
need not be configured, and a configured registry shows here only once
it has been fetched. ctxt config show prints the configured list.`,
	RunE: runRegistryList,
}

var registryAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Fetch and cache a registry's manifest",
	Long: `Fetch the manifest for the registry at <url> and cache it locally,
so it shows in ctxt registry list. Idempotent: a repeat call overwrites
the cached manifest.

add does not write the config file, and <name> only labels the output.
sync, info, login and delete find a registry by name in the config
file's registries list: add an entry there to subscribe (ctxt config
edit opens the file):

  registries:
    - name: uxpatterns
      url: https://uxpatterns.example.com`,
	Args: cobra.ExactArgs(2),
	RunE: runRegistryAdd,
}

var registryRemoveCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Remove a registry",
	Long: `Drop the named registry from the local cache and remove the
matching entry from the config file. Fails when the registry is not
present in the config.

` + cliconv.DestructiveTokenHelp,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryRemove,
}

var registryInfoCmd = &cobra.Command{
	Use:   "info <name>",
	Short: "Show registry information",
	Long: `Display the cached manifest metadata for a registry: URL, last
fetch timestamp, ETag, signing key fingerprint, trust status, and
the high-level manifest fields (name, version, description, step
count). Read-only.`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryInfo,
}

var registrySyncCmd = &cobra.Command{
	Use:   "sync <name>",
	Short: "Sync registry metadata",
	Long: `Sync registry metadata from the remote source: fetch the registry's
manifest from its url and replace the cached copy, so Last Fetched and
the ETag advance (see ctxt registry info). When the manifest advertises
capabilities.entity_sync, also sync the registry's entity index
(/entities/index) into the local entity store; a registry that serves
steps only is not asked for entities. A registry with auth.type gets the
token ctxt registry login stored.

With --dry-run, fetches the remote manifest and shows a diff of would-be
additions, updates, and removals without writing anything to storage.`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistrySync,
}

var registrySubmitCmd = &cobra.Command{
	Use:   "submit <bundle-path>",
	Short: "Submit a bundle to the community registry (prints PR instructions)",
	Long: `Validate a local registry bundle (file or directory with a
manifest.json) and print the pull-request instructions for the
community registry. No network calls are made; the command is a
guided stub for community submission.`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistrySubmit,
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

var registryEntitlementsCmd = &cobra.Command{
	Use:   "entitlements",
	Short: "List active entitlements per registry",
	Long: `List entitlement records fetched from registries that declare an /entitlements endpoint.

Examples:
  ctxt registry entitlements
  ctxt registry entitlements --format json`,
	RunE: runRegistryEntitlements,
}

var registryUsageCmd = &cobra.Command{
	Use:   "usage [name]",
	Short: "Show metering usage for registries",
	Long: `Show metering event counts per registry for the current billing period.

Without arguments, shows all registries. Pass a registry name to filter.

Examples:
  # Show all registry usage
  ctxt registry usage

  # Show usage for a specific registry
  ctxt registry usage example-paid

  # Output as JSON
  ctxt registry usage --format json

  # Show usage for a custom period (days back from now)
  ctxt registry usage --days 30`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRegistryUsage,
}

var registrySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search the registry index for relevant namespaces and registries",
	Long: `Query the remote registry index (reference registry) for registries
matching a topic, namespace prefix, or tag.

Examples:
  # Find registries related to AI topics
  ctxt registry search ai

  # Find registries by namespace prefix
  ctxt registry search frontend

  # Output as JSON
  ctxt registry search react --format json

  # Use a custom index URL
  ctxt registry search devops --index-url https://myorg.example.com/registry-index.json`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistrySearch,
}

var registryLoginCmd = &cobra.Command{
	Use:   "login <name>",
	Short: "Store an auth token for a registry in the OS keychain",
	Long: `Store an auth token for a registry securely in the OS keychain.

The token is NEVER written to the YAML config file.

The token is bound to the origin (scheme, host, port) of the registry's
url and is sent only there; login prints that origin. When the registry
list comes from a project .contexthelp/ctxt.yaml (a cloned repository can
ship one), login asks before binding; non-interactively it refuses unless
--origin names that origin.

Interactive (prompts for token):
  ctxt registry login example-paid

Non-interactive (pass token via flag — prefer env-var to avoid shell history):
  ctxt registry login example-paid --token "$TOKEN"

Pin the origin (required non-interactively for a project-config registry):
  ctxt registry login example-paid --token "$TOKEN" --origin https://registry.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryLogin,
}

var registryLogoutCmd = &cobra.Command{
	Use:   "logout <name>",
	Short: "Remove the stored auth token for a registry from the OS keychain",
	Long: `Delete the auth token for the named registry from the OS
keychain. Fails when no token is stored for that registry. The
config file is not modified; only the keychain entry is removed.`,
	Args: cobra.ExactArgs(1),
	RunE: runRegistryLogout,
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
	registryCmd.AddCommand(registryEntitlementsCmd)
	registryCmd.AddCommand(registryUsageCmd)
	registryCmd.AddCommand(registrySearchCmd)

	// Flags for search subcommand
	registrySearchCmd.Flags().String("index-url", registry.DefaultIndexURL,
		"URL of the registry index to search")

	// Flags for usage subcommand
	registryUsageCmd.Flags().Int("days", 0,
		"period in days back from now (0 = all time)")

	// Flags for sync subcommand. --dry-run is inherited from the kit
	// global persistent flag; do not re-register here (12fcc local-globals).
	registrySyncCmd.Flags().Bool("reconcile", false,
		"sync all configured registries with multi-registry entity reconciliation")
	registrySyncCmd.Flags().String("merge-strategy", "last-write-wins",
		"merge strategy for entity conflicts: last-write-wins or trust-score")

	// Flags for login subcommand
	registryLoginCmd.Flags().String("token", "",
		"auth token (reads from stdin prompt if omitted)")
	registryLoginCmd.Flags().String("origin", "",
		"origin the token is bound to (scheme://host[:port]); must match the registry url. Required to log in non-interactively when the registry comes from a project .contexthelp/ctxt.yaml")

	cliconv.WithSideEffect(registryListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(registryListCmd, []cliconv.Example{
		{Title: "List cached registry manifests", Command: "ctxt registry list"},
		{Title: "List as JSON", Command: "ctxt registry list --format json"},
	})

	cliconv.WithSideEffect(registryAddCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(registryAddCmd, []cliconv.Example{
		{Title: "Add a registry by URL", Command: "ctxt registry add uxpatterns https://uxpatterns.example.com"},
		{Title: "Add then list cached registries", Command: "ctxt registry add uxpatterns https://uxpatterns.example.com && ctxt registry list"},
	})
	cliconv.WithNextSteps(registryAddCmd, []cliconv.NextStep{
		{Suggest: "ctxt config edit", Reason: "add a registries entry (name, url) so sync, info, login and delete find the registry"},
		{Suggest: "ctxt registry sync <name>", Reason: "once configured: refresh the manifest, plus the entity index when the manifest advertises capabilities.entity_sync"},
	})

	cliconv.WithSideEffect(registryRemoveCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(registryRemoveCmd)
	cliconv.WithExamples(registryRemoveCmd, []cliconv.Example{
		{Title: "Delete a registry", Command: "ctxt registry delete uxpatterns --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(registryRemoveCmd, []cliconv.NextStep{
		{Suggest: "ctxt config show", Reason: "confirm the entry is gone from the configured registries"},
		{Suggest: "ctxt registry logout <name>", Reason: "drop any leftover keychain token tied to the registry"},
	})

	cliconv.WithSideEffect(registryInfoCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(registryInfoCmd, []cliconv.Example{
		{Title: "Show registry info", Command: "ctxt registry info uxpatterns"},
		{Title: "Emit info as JSON", Command: "ctxt registry info uxpatterns --format json"},
	})

	cliconv.WithSideEffect(registrySyncCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(registrySyncCmd, []cliconv.Example{
		{Title: "Sync a registry", Command: "ctxt registry sync uxpatterns"},
		{Title: "Preview without writing", Command: "ctxt registry sync --dry-run uxpatterns"},
	})
	cliconv.WithNextSteps(registrySyncCmd, []cliconv.NextStep{
		{Suggest: "ctxt registry info <name>", Reason: "verify Last Fetched timestamp and ETag advanced"},
		{When: "if conflicts were reported", Suggest: "ctxt registry sync --reconcile --merge-strategy trust-score", Reason: "reconcile entity conflicts across registries"},
	})

	cliconv.WithSideEffect(registrySubmitCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(registrySubmitCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registrySubmitCmd, []cliconv.Example{
		{Title: "Validate a bundle directory", Command: "ctxt registry submit ./mybundle"},
		{Title: "Emit PR instructions as JSON", Command: "ctxt registry submit ./mybundle --format json"},
	})
	cliconv.WithNextSteps(registrySubmitCmd, []cliconv.NextStep{
		{Suggest: "open https://github.com/ideacrafterslabs/registry", Reason: "follow the printed PR template to upstream the bundle"},
		{Suggest: "ctxt registry info <name>", Reason: "double-check the manifest fields before submitting"},
	})

	cliconv.WithSideEffect(registryLoginCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(registryLoginCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryLoginCmd, []cliconv.Example{
		{Title: "Login interactively", Command: "ctxt registry login example-paid"},
		{Title: "Login with env-var token", Command: "ctxt registry login example-paid --token \"$REGISTRY_TOKEN\""},
	})
	cliconv.WithNextSteps(registryLoginCmd, []cliconv.NextStep{
		{Suggest: "ctxt registry sync <name>", Reason: "the token unlocks authenticated sync; pull fresh entities now"},
		{Suggest: "ctxt registry entitlements", Reason: "check which plan and namespaces the token grants"},
	})

	cliconv.WithSideEffect(registryLogoutCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(registryLogoutCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryLogoutCmd, []cliconv.Example{
		{Title: "Remove a stored token", Command: "ctxt registry logout example-paid"},
		{Title: "Logout then verify", Command: "ctxt registry logout example-paid && ctxt registry entitlements"},
	})
	cliconv.WithNextSteps(registryLogoutCmd, []cliconv.NextStep{
		{Suggest: "ctxt registry entitlements", Reason: "confirm the authenticated entitlement row is gone"},
		{When: "to re-authenticate later", Suggest: "ctxt registry login <name>", Reason: "store a fresh token in the keychain"},
	})

	cliconv.WithSideEffect(registryCapabilitiesCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(registryCapabilitiesCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryCapabilitiesCmd, []cliconv.Example{
		{Title: "Show capability handshake", Command: "ctxt registry capabilities default"},
		{Title: "Capabilities for a named registry", Command: "ctxt registry capabilities uxpatterns"},
	})

	cliconv.WithSideEffect(registryEntitlementsCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(registryEntitlementsCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryEntitlementsCmd, []cliconv.Example{
		{Title: "List active entitlements", Command: "ctxt registry entitlements"},
		{Title: "Emit entitlements as JSON", Command: "ctxt registry entitlements --format json"},
	})

	cliconv.WithSideEffect(registryUsageCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(registryUsageCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryUsageCmd, []cliconv.Example{
		{Title: "Show all registry usage", Command: "ctxt registry usage"},
		{Title: "Show usage for last 30 days", Command: "ctxt registry usage example-paid --days 30"},
	})

	cliconv.WithSideEffect(registrySearchCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(registrySearchCmd, []cliconv.Example{
		{Title: "Search registries by topic", Command: "ctxt registry search ai"},
		{Title: "Search and emit JSON", Command: "ctxt registry search react --format json"},
	})
}

func runRegistryList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := asOwner(cmdContext(cmd))
	registries, total, err := svc.ListRegistries(ctx)
	if err != nil {
		return fmt.Errorf("list registries: %w", err)
	}

	if isJSONOutput() {
		shown := make([]*storage.RegistryCache, len(registries))
		for i, r := range registries {
			shown[i] = redactedRegistryCache(r)
		}
		return outputJSON(os.Stdout, map[string]any{"registries": shown, "total": total})
	}

	rr := make([]registryRow, 0, len(registries))
	for _, r := range registries {
		rr = append(rr, registryRow{
			URL:         config.RedactDSN(r.RegistryURL),
			LastFetched: r.LastFetched.Format("2006-01-02 15:04"),
		})
	}
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), rr)
	}

	fmt.Printf("Registries (%d)\n\n", total)
	if total == 0 {
		fmt.Println("  No registry manifests cached.")
		fmt.Println("  Fetch one with: ctxt registry add <name> <url>")
		return nil
	}
	headers := []string{"URL", "Last Fetched"}
	rows := make([][]string, 0, len(rr))
	for _, r := range rr {
		rows = append(rows, []string{r.URL, r.LastFetched})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

// registryRow is the row shape `registry list` projects for table, csv
// and text. The URL is redacted, as in every other view.
type registryRow struct {
	URL         string `table:"URL"`
	LastFetched string `table:"Last Fetched"`
}

func runRegistryAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := args[1]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := asOwner(cmdContext(cmd))
	fmt.Printf("Adding registry %s (%s)...\n", name, config.RedactDSN(url))
	if err := svc.FetchRegistry(ctx, url); err != nil {
		return fmt.Errorf("fetch registry: %w", err)
	}
	fmt.Println("Registry manifest cached")
	if !registryConfigured(name, url) {
		fmt.Printf("Not in config: add a registries entry (name: %s, url: %s) with ctxt config edit so sync, info, login and delete find it\n",
			name, config.RedactDSN(url))
	}
	return nil
}

// redactedRegistryCache returns a copy of c for display: its
// RegistryURL masks any credential the configured url embeds.
func redactedRegistryCache(c *storage.RegistryCache) *storage.RegistryCache {
	if c == nil {
		return nil
	}
	shown := *c
	shown.RegistryURL = config.RedactDSN(c.RegistryURL)
	return &shown
}

// registryConfigured reports whether the loaded config's registries list
// has an entry with this name and url.
func registryConfigured(name, url string) bool {
	for _, r := range cfg.Registries {
		if r.Name == name && r.URL == url {
			return true
		}
	}
	return false
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

	// Drop the entry from the write target's own layer. It is staged
	// before the cache is touched, so an entry that another config file
	// sets fails the command without deleting anything.
	layer, err := config.OpenLayer(configPath())
	if err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	removed, err := layer.RemoveItems(func(item *yaml.Node) (bool, error) {
		var r config.RegistryConfig
		if err := item.Decode(&r); err != nil {
			return false, err
		}
		return r.Name == name, nil
	}, "registries")
	if err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	if removed == 0 {
		return errNotInLayer("registry", name, layer.Path())
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := asOwner(cmdContext(cmd))
	if err := svc.RemoveRegistry(ctx, registryURL); err != nil {
		return fmt.Errorf("remove registry cache: %w", err)
	}

	if err := layer.Save(); err != nil {
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

	ctx := asOwner(cmdContext(cmd))
	cache, err := svc.Store(ctx).Registries().GetCachedManifest(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("get registry cache: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, redactedRegistryCache(cache))
	}

	fmt.Printf("Registry: %s\n\n", name)
	fmt.Printf("URL:          %s\n", config.RedactDSN(cache.RegistryURL))
	fmt.Printf("Last Fetched: %s\n", cache.LastFetched.Format("2006-01-02 15:04:05"))
	if cache.ETag != "" {
		fmt.Printf("ETag:         %s\n", cache.ETag)
	}
	if cache.Manifest != nil {
		fmt.Printf("Name:         %s\n", cache.Manifest.Name)
		fmt.Printf("Version:      %s\n", cache.Manifest.Version)
		fmt.Printf("Description:  %s\n", cache.Manifest.Description)
		fmt.Printf("Steps:        %d\n", len(cache.Manifest.Steps))
		if cache.Manifest.PublicKey != "" {
			fmt.Printf("Public Key:   %s\n", cache.Manifest.PublicKey)
		}
	}
	// Trust status.
	trustStatus := string(cache.TrustStatus)
	if trustStatus == "" {
		trustStatus = "unknown"
	}
	fmt.Printf("Trust Status: %s\n", trustStatus)
	if cache.KeyFingerprint != "" {
		fmt.Printf("Key Fingerprint: %s\n", cache.KeyFingerprint)
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

	ctx := asOwner(cmdContext(cmd))

	if dryRun {
		return runRegistrySyncDryRun(ctx, svc, name, registryURL)
	}

	if reconcile {
		return runRegistrySyncReconcile(ctx, cmd, svc, registry.MergeStrategy(mergeStrategy))
	}

	w := cmd.OutOrStdout()
	if !isJSONOutput() {
		fmt.Fprintf(w, "Syncing registry %s...\n", name)
	}
	res, err := svc.SyncRegistry(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("sync registry: %w", err)
	}
	if isJSONOutput() {
		return outputJSON(w, map[string]any{
			"registry":         name,
			"url":              config.RedactDSN(registryURL),
			"entity_sync":      res.EntitySync,
			"trust_level":      res.TrustLevel,
			"upserted":         res.Upserted,
			"pending_approval": res.PendingApproval,
			"blocked":          res.Blocked,
		})
	}
	fmt.Fprintln(w, "Registry synced")
	if !res.EntitySync {
		fmt.Fprintln(w, "Entities: none (manifest does not advertise capabilities.entity_sync)")
		return nil
	}
	fmt.Fprintf(w, "Entities synced: %d\n", res.Upserted)
	printHeldEntities(w, name, res.RegistryEntityCounts)
	return nil
}

// printHeldEntities says how many entities the registry's trust_level
// kept out of the local store, and why, so an untrusted registry does
// not read as an empty one: an untrusted registry's wait in the approval
// queue, a sandboxed one's are blocked. Prints nothing when none were
// held.
func printHeldEntities(w io.Writer, name string, c service.RegistryEntityCounts) {
	level := "trust_level " + string(c.TrustLevel)
	for _, r := range cfg.Registries {
		if r.Name == name && r.TrustLevel == "" {
			level = "trust_level unset, so untrusted"
			break
		}
	}
	if c.PendingApproval > 0 {
		fmt.Fprintf(w, "Entities not written from %s: %d pending approval (ctxt registry pending --registry %s)\n",
			name, c.PendingApproval, name)
		fmt.Fprintf(w, "  registry not trusted (%s): review them, then ctxt registry approve or reject;\n", level)
		fmt.Fprintln(w, "  or set trust_level: trusted on its registries entry (ctxt config edit) and sync again.")
	}
	if c.Blocked > 0 {
		fmt.Fprintf(w, "Entities not written from %s: %d blocked (registry sandboxed: %s never writes to the local graph)\n",
			name, c.Blocked, level)
	}
}

// runRegistrySyncReconcile syncs all configured registries with multi-registry
// entity reconciliation and prints the conflict report.
func runRegistrySyncReconcile(
	ctx context.Context,
	cmd *cobra.Command,
	svc *service.Service,
	strategy registry.MergeStrategy,
) error {
	w := cmd.OutOrStdout()
	if !isJSONOutput() {
		fmt.Fprintf(w, "Syncing all registries with reconciliation (strategy: %s)...\n", strategy)
	}

	result, err := svc.SyncAllRegistriesWithReconciliation(ctx, strategy, nil)
	if err != nil {
		return fmt.Errorf("reconcile sync: %w", err)
	}

	if isJSONOutput() {
		offline := make([]string, len(result.OfflineRegistries))
		for i, u := range result.OfflineRegistries {
			offline[i] = config.RedactDSN(u)
		}
		return outputJSON(w, map[string]any{
			"merged":             result.Merged,
			"pending_approval":   result.PendingApproval,
			"blocked":            result.Blocked,
			"registries":         result.Registries,
			"conflicts":          result.Conflicts,
			"offline_registries": offline,
		})
	}

	fmt.Fprintf(w, "Entities merged: %d\n", result.Merged)
	for _, r := range result.Registries {
		printHeldEntities(w, r.Name, service.RegistryEntityCounts{
			TrustLevel: r.TrustLevel, PendingApproval: r.PendingApproval, Blocked: r.Blocked,
		})
	}

	if len(result.OfflineRegistries) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "\nOffline registries (cached values preserved):\n")
		for _, url := range result.OfflineRegistries {
			fmt.Fprintf(cmd.OutOrStdout(), "  OFFLINE  %s\n", config.RedactDSN(url))
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
	ctx context.Context,
	svc *service.Service,
	name, registryURL string,
) error {
	if !isJSONOutput() {
		fmt.Printf("Dry-run: fetching remote manifest for %s...\n\n", name)
	}

	diffs, err := svc.DiffRegistrySync(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("diff registry: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"registry": name,
			"url":      config.RedactDSN(registryURL),
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

// runRegistryUsage prints metering usage totals per registry per event type.
func runRegistryUsage(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	days, err := cmd.Flags().GetInt("days")
	if err != nil {
		return fmt.Errorf("days flag: %w", err)
	}

	var periodStart time.Time
	if days > 0 {
		periodStart = time.Now().UTC().AddDate(0, 0, -days)
	}

	ctx := asOwner(cmdContext(cmd))

	var registryName string
	if len(args) > 0 {
		registryName = args[0]
	}

	var aggs []*storage.MeteringAggregate
	if registryName != "" {
		aggs, err = svc.RegistryUsageSummary(ctx, registryName, periodStart)
	} else {
		aggs, err = svc.AllRegistriesUsageSummary(ctx, periodStart)
	}
	if err != nil {
		return fmt.Errorf("usage summary: %w", err)
	}

	type usageRow struct {
		RegistryName string `json:"registry_name"`
		EventType    string `json:"event_type"`
		Count        int    `json:"count"`
	}

	rows := make([]usageRow, 0, len(aggs))
	for _, a := range aggs {
		rows = append(rows, usageRow{
			RegistryName: a.RegistryName,
			EventType:    string(a.EventType),
			Count:        a.Total,
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"usage":        rows,
			"period_start": periodStart,
		})
	}

	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No metering events recorded.")
		return nil
	}

	headers := []string{"Registry", "Event Type", "Count"}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{r.RegistryName, r.EventType, fmt.Sprintf("%d", r.Count)})
	}
	printTable(cmd.OutOrStdout(), headers, tableRows)
	return nil
}

// runRegistryLogin stores an auth token for the named registry in the OS keychain.
// Token source priority: --token flag > interactive prompt.
func runRegistryLogin(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Validate registry exists in config.
	reg, ok := registryConfigByName(name)
	if !ok {
		return fmt.Errorf("registry %q not found in config — add it first with: ctxt registry add %s <url>", name, name)
	}
	// The token is bound to this origin: a config that later points the
	// name elsewhere does not get it.
	origin, err := registry.CanonicalOrigin(reg.URL)
	if err != nil {
		return fmt.Errorf("registry login: registry %q: %w", name, err)
	}

	token, err := cmd.Flags().GetString("token")
	if err != nil {
		return fmt.Errorf("token flag: %w", err)
	}
	pinned, err := cmd.Flags().GetString("origin")
	if err != nil {
		return fmt.Errorf("origin flag: %w", err)
	}

	in := bufio.NewReader(loginInput)
	if err := confirmLoginOrigin(cmd, in, name, origin, pinned); err != nil {
		return err
	}

	if token == "" {
		token, err = readToken(cmd, in)
		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("token must not be empty")
	}

	store := registry.NewTokenStore()
	if err := store.Set(name, origin, token); err != nil {
		return fmt.Errorf("registry login: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Token stored in keychain for registry %q; token bound to %s\n", name, origin)
	if reg.Auth.Type == "" {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"note: registry %q has no auth.type, so the token is not sent; set registries[].auth.type (bearer | api_key | oauth2)\n",
			name)
	}
	if registry.InsecureOrigin(origin) {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"note: registry %q is plain http; the token is sent only over https or to a loopback host\n", name)
	}
	return nil
}

// loginInput is where `registry login` reads the origin confirmation
// and the prompted token; loginInteractive reports whether it is a
// terminal. Tests replace both.
var (
	loginInput       io.Reader = os.Stdin
	loginInteractive           = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
)

// confirmLoginOrigin checks the origin `registry login` is about to bind
// the token to. --origin pins it: it must name the registry url's own
// origin. Without it, a registry list read from the project layer (a
// `.contexthelp/ctxt.yaml` in the working tree, which a cloned repository
// controls) needs a typed yes; non-interactively the login is refused.
func confirmLoginOrigin(cmd *cobra.Command, in *bufio.Reader, name, origin, pinned string) error {
	if pinned != "" {
		want, err := registry.CanonicalOrigin(pinned)
		if err != nil {
			return fmt.Errorf("registry login: --origin: %w", err)
		}
		if want != origin {
			return fmt.Errorf("registry login: --origin %s does not match registry %q, whose url is at %s", want, name, origin)
		}
		return nil
	}

	paths, _, _ := root.ConfigArgs()
	src, found, err := config.RegistriesSource(binName, "", paths)
	if err != nil {
		return fmt.Errorf("registry login: %w", err)
	}
	if !found || src.Kind != config.LayerProject {
		return nil
	}
	if !loginInteractive() {
		return fmt.Errorf("registry login: registry %q url comes from the project config %s; "+
			"to bind a token to %s, pass --origin %s", name, src.Path, origin, origin)
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Registry %q url comes from the project config %s.\nBind the token to %s? [y/N] ", name, src.Path, origin)
	answer, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("registry login: read confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", confirmYes:
		return nil
	default:
		return fmt.Errorf("registry login: not confirmed; no token stored")
	}
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

// registryConfigByName returns the cfg.Registries entry named name.
func registryConfigByName(name string) (config.RegistryConfig, bool) {
	if cfg == nil {
		return config.RegistryConfig{}, false
	}
	for _, r := range cfg.Registries {
		if r.Name == name {
			return r, true
		}
	}
	return config.RegistryConfig{}, false
}

// defaultRegistryName names the bundled default registry
// (registry.DefaultRegistryURL) on the command line.
const defaultRegistryName = "default"

// runRegistryCapabilities fetches the cached manifest for name and runs the
// capability handshake, printing any version or feature warnings.
func runRegistryCapabilities(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Resolve URL: "default" maps to the bundled sentinel URL.
	registryURL := registry.DefaultRegistryURL
	if name != defaultRegistryName {
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

	ctx := asOwner(cmdContext(cmd))
	warnings, err := svc.CheckRegistryCapabilities(ctx, registryURL, version,
		"entity_sync", "taxonomy", "translations",
	)
	if err != nil {
		return fmt.Errorf("capability check: %w", err)
	}

	cache, err := svc.Store(ctx).Registries().GetCachedManifest(ctx, registryURL)
	if err != nil {
		return fmt.Errorf("get registry: %w", err)
	}

	type capResult struct {
		Name         string              `json:"name"`
		URL          string              `json:"url"`
		Version      string              `json:"version"`
		MinClient    string              `json:"min_client_version,omitempty"`
		Capabilities map[string]bool     `json:"capabilities"`
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

	warnMaps := make([]map[string]string, 0, len(warnings))
	for _, w := range warnings {
		warnMaps = append(warnMaps, map[string]string{
			"feature": w.Feature,
			"message": w.Message,
		})
	}

	result := capResult{
		Name:         name,
		URL:          config.RedactDSN(registryURL),
		Version:      mVersion,
		MinClient:    mMinClient,
		Capabilities: caps,
		Warnings:     warnMaps,
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), result)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Registry: %s\n", name)
	fmt.Fprintf(cmd.OutOrStdout(), "URL:      %s\n", result.URL)
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

// runRegistrySearch queries the remote registry index and prints matching registries.
func runRegistrySearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	indexURL, err := cmd.Flags().GetString("index-url")
	if err != nil {
		return fmt.Errorf("index-url flag: %w", err)
	}

	// An index served by a configured registry gets that registry's token.
	var regs []config.RegistryConfig
	if cfg != nil {
		regs = cfg.Registries
	}
	client := registry.NewCredentials(regs, registry.KeychainTokens()).
		Client(&http.Client{Timeout: 15 * time.Second})

	ctx := asOwner(cmdContext(cmd))
	idx, err := registry.FetchIndex(ctx, client, indexURL)
	if err != nil {
		return fmt.Errorf("fetch registry index: %w", err)
	}

	results := registry.SearchIndex(idx, query)

	if isJSONOutput() {
		type jsonResult struct {
			Name        string   `json:"name"`
			URL         string   `json:"url"`
			Description string   `json:"description"`
			Namespaces  []string `json:"namespaces,omitempty"`
			Tags        []string `json:"tags,omitempty"`
			TrustLevel  string   `json:"trust_level,omitempty"`
			EntityCount int      `json:"entity_count,omitempty"`
			Version     string   `json:"version,omitempty"`
		}
		out := make([]jsonResult, 0, len(results))
		for _, r := range results {
			out = append(out, jsonResult{
				Name:        r.Entry.Name,
				URL:         r.Entry.URL,
				Description: r.Entry.Description,
				Namespaces:  r.Entry.Namespaces,
				Tags:        r.Entry.Tags,
				TrustLevel:  r.Entry.TrustLevel,
				EntityCount: r.Entry.EntityCount,
				Version:     r.Entry.Version,
			})
		}
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"query":   query,
			"results": out,
			"total":   len(out),
		})
	}

	if len(results) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No registries found matching %q.\n", query)
		fmt.Fprintln(cmd.OutOrStdout(), "Try a broader query or visit: "+indexURL)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Registry search results for %q (%d found):\n\n", query, len(results))

	headers := []string{"Name", "URL", "Namespaces", "Tags", "Trust", "Entities"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			r.Entry.Name,
			r.Entry.URL,
			strings.Join(r.Entry.Namespaces, ", "),
			strings.Join(r.Entry.Tags, ", "),
			r.Entry.TrustLevel,
			fmt.Sprintf("%d", r.Entry.EntityCount),
		})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

// readToken prompts the user for a token read from stdin.
// In interactive sessions the token is read line-by-line; input is not echoed
// because the prompt instructs the user to paste rather than type.
// For masked input in production use --token flag with an env-var reference.
func readToken(cmd *cobra.Command, in *bufio.Reader) (string, error) {
	fmt.Fprint(cmd.OutOrStdout(), "Enter token: ")
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read token: %w", err)
	}
	if line == "" {
		return "", fmt.Errorf("no token provided on stdin")
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// runRegistryEntitlements lists active entitlement records stored locally.
func runRegistryEntitlements(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := asOwner(cmdContext(cmd))
	entitlements, err := svc.Store(ctx).Entitlements().List(ctx, storage.EntitlementOutbound)
	if err != nil {
		return fmt.Errorf("list entitlements: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"entitlements": entitlements})
	}

	if len(entitlements) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No entitlement records found.")
		fmt.Fprintln(cmd.OutOrStdout(), "Entitlements are fetched automatically on sync when a registry declares an /entitlements endpoint.")
		return nil
	}

	headers := []string{"Registry", "Plan", "Namespaces", "Expires", "Fetched"}
	rows := make([][]string, 0, len(entitlements))
	for _, e := range entitlements {
		expires := "never"
		if !e.ExpiresAt.IsZero() {
			expires = e.ExpiresAt.Format("2006-01-02")
		}
		rows = append(rows, []string{
			e.RegistryName,
			e.Plan,
			strings.Join(e.Namespaces, ", "),
			expires,
			e.FetchedAt.Format(time.RFC3339),
		})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}
