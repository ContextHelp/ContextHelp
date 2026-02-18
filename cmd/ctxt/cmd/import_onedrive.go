package cmd

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	onedriveimporter "github.com/ideacrafterslabs/ctxt/internal/importer/onedrive"
	"github.com/spf13/cobra"
)

type oneDriveClient interface {
	ListDriveFolderItems(ctx context.Context, driveID, folderPath string, maxItems int) ([]onedriveimporter.Item, error)
	GetItem(ctx context.Context, driveID, itemID string) (onedriveimporter.Item, error)
	ListSharedWithMe(ctx context.Context, maxItems int) ([]onedriveimporter.Item, error)
}

var newOneDriveClient = func(token, baseURL string) oneDriveClient {
	return onedriveimporter.NewClient(nil, baseURL, token)
}

var enqueueOneDriveContent = enqueueContent

var importOneDriveCmd = &cobra.Command{
	Use:   "onedrive",
	Short: "Import files from Microsoft OneDrive/SharePoint",
	Long: `Import files from Microsoft OneDrive and SharePoint document libraries.

Required from user:
  1) Microsoft Graph access token:
     --token <token> or ONEDRIVE_TOKEN env var
     Expected scopes:
       Files.Read (or Files.Read.All for org-wide/library access)
       Sites.Read.All (required for SharePoint libraries)

  2) At least one scope selector:
     --drive-id <id>             import root of a drive (repeatable)
     --drive-folder <id>:/path   import a specific folder (repeatable)
     --item-ref <id>/<item-id>   import specific item(s) (repeatable)
     --shared-with-me            import files shared with the user

Selective controls:
  --since <RFC3339>             only include items modified since timestamp
  --include-ext <.ext>          include only these file extensions (repeatable)
  --max-items <n>               cap selected items

Examples:
  # Dry-run root import for a drive
  ctxt import onedrive --drive-id b!abc123 --dry-run

  # Folder-scoped import with filters
  ctxt import onedrive \
    --drive-folder b!abc123:/Projects/2026 \
    --since 2026-01-01T00:00:00Z \
    --include-ext .pdf --include-ext .docx

  # Import explicit item references and enqueue
  ctxt import onedrive \
    --item-ref b!abc123/01XQ7M3P5H2QABCD1234EFGH5678IJKL \
    --server http://localhost:8080`,
	RunE: runImportOneDrive,
}

func init() {
	importCmd.AddCommand(importOneDriveCmd)

	importOneDriveCmd.Flags().String("token", "", "Microsoft Graph access token (or ONEDRIVE_TOKEN env var)")
	importOneDriveCmd.Flags().StringSlice("drive-id", nil, "drive ID to import from root (repeatable)")
	importOneDriveCmd.Flags().StringSlice("drive-folder", nil, "folder scope as <drive-id>:/path (repeatable)")
	importOneDriveCmd.Flags().StringSlice("item-ref", nil, "specific item as <drive-id>/<item-id> (repeatable)")
	importOneDriveCmd.Flags().Bool("shared-with-me", false, "include files from /me/drive/sharedWithMe")
	importOneDriveCmd.Flags().String("since", "", "only import files modified since RFC3339 timestamp")
	importOneDriveCmd.Flags().StringSlice("include-ext", nil, "file extension filter, e.g. .pdf (repeatable)")
	importOneDriveCmd.Flags().Int("max-items", 0, "maximum files to import (0 = all)")

	importOneDriveCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importOneDriveCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	importOneDriveCmd.Flags().Bool("dry-run", false, "list selected files without enqueueing jobs")

	// For testing and self-hosted proxies.
	importOneDriveCmd.Flags().String("graph-base-url", "", "override Microsoft Graph API base URL")
	_ = importOneDriveCmd.Flags().MarkHidden("graph-base-url")
}

type driveFolderScope struct {
	DriveID string
	Path    string
}

type itemScope struct {
	DriveID string
	ItemID  string
}

func runImportOneDrive(cmd *cobra.Command, args []string) error {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token = os.Getenv("ONEDRIVE_TOKEN")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("onedrive token is required (use --token or ONEDRIVE_TOKEN)")
	}

	driveIDs, _ := cmd.Flags().GetStringSlice("drive-id")
	driveFolderRefs, _ := cmd.Flags().GetStringSlice("drive-folder")
	itemRefs, _ := cmd.Flags().GetStringSlice("item-ref")
	sharedWithMe, _ := cmd.Flags().GetBool("shared-with-me")
	sinceRaw, _ := cmd.Flags().GetString("since")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	includeExtRaw, _ := cmd.Flags().GetStringSlice("include-ext")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if len(driveIDs) == 0 && len(driveFolderRefs) == 0 && len(itemRefs) == 0 && !sharedWithMe {
		return fmt.Errorf("provide at least one scope: --drive-id, --drive-folder, --item-ref, or --shared-with-me")
	}

	var since *time.Time
	if strings.TrimSpace(sinceRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return fmt.Errorf("invalid --since timestamp: %w", err)
		}
		since = &parsed
	}

	includeExt, err := normalizeExtensions(includeExtRaw)
	if err != nil {
		return err
	}

	driveFolders, err := parseDriveFolderScopes(driveFolderRefs)
	if err != nil {
		return err
	}
	items, err := parseItemScopes(itemRefs)
	if err != nil {
		return err
	}

	serverURL, _ := cmd.Flags().GetString("server")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	graphBaseURL, _ := cmd.Flags().GetString("graph-base-url")

	client := newOneDriveClient(token, graphBaseURL)
	ctx := context.Background()

	itemsByKey := make(map[string]onedriveimporter.Item)
	orderedKeys := make([]string, 0)
	remaining := func() int {
		if maxItems <= 0 {
			return 0
		}
		return maxItems - len(orderedKeys)
	}

	var (
		scanned int
		skipped int
	)

	addItem := func(item onedriveimporter.Item) {
		scanned++
		if !item.IsFile {
			skipped++
			return
		}
		if since != nil && !item.LastModifiedTime.IsZero() && item.LastModifiedTime.Before(*since) {
			skipped++
			return
		}
		if len(includeExt) > 0 {
			ext := strings.ToLower(path.Ext(strings.TrimSpace(item.Name)))
			if _, ok := includeExt[ext]; !ok {
				skipped++
				return
			}
		}

		key := strings.TrimSpace(item.DriveID) + ":" + strings.TrimSpace(item.ID)
		if key == ":" {
			skipped++
			return
		}
		if _, exists := itemsByKey[key]; exists {
			return
		}

		itemsByKey[key] = item
		orderedKeys = append(orderedKeys, key)
	}

	for _, driveID := range driveIDs {
		if maxItems > 0 && len(orderedKeys) >= maxItems {
			break
		}
		limit := remaining()
		found, err := client.ListDriveFolderItems(ctx, strings.TrimSpace(driveID), "/", limit)
		if err != nil {
			return fmt.Errorf("list drive %s root: %w", strings.TrimSpace(driveID), err)
		}
		for _, item := range found {
			addItem(item)
			if maxItems > 0 && len(orderedKeys) >= maxItems {
				break
			}
		}
	}

	for _, scope := range driveFolders {
		if maxItems > 0 && len(orderedKeys) >= maxItems {
			break
		}
		limit := remaining()
		found, err := client.ListDriveFolderItems(ctx, scope.DriveID, scope.Path, limit)
		if err != nil {
			return fmt.Errorf("list drive folder %s:%s: %w", scope.DriveID, scope.Path, err)
		}
		for _, item := range found {
			addItem(item)
			if maxItems > 0 && len(orderedKeys) >= maxItems {
				break
			}
		}
	}

	for _, scope := range items {
		if maxItems > 0 && len(orderedKeys) >= maxItems {
			break
		}
		item, err := client.GetItem(ctx, scope.DriveID, scope.ItemID)
		if err != nil {
			return fmt.Errorf("get item %s/%s: %w", scope.DriveID, scope.ItemID, err)
		}
		addItem(item)
	}

	if sharedWithMe && (maxItems <= 0 || len(orderedKeys) < maxItems) {
		limit := remaining()
		found, err := client.ListSharedWithMe(ctx, limit)
		if err != nil {
			return fmt.Errorf("list shared-with-me files: %w", err)
		}
		for _, item := range found {
			addItem(item)
			if maxItems > 0 && len(orderedKeys) >= maxItems {
				break
			}
		}
	}

	if len(orderedKeys) == 0 {
		return fmt.Errorf("no onedrive files selected for import")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("OneDrive items selected: %d (dry-run)\n", len(orderedKeys))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(orderedKeys)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			item := itemsByKey[orderedKeys[i]]
			location := item.Path
			if strings.TrimSpace(location) == "" {
				location = item.Name
			}
			if item.WebURL == "" {
				fmt.Printf("%d. [%s] %s\n", i+1, item.DriveID, location)
			} else {
				fmt.Printf("%d. [%s] %s -> %s\n", i+1, item.DriveID, location, item.WebURL)
			}
		}

		if len(orderedKeys) > previewLimit {
			fmt.Printf("... and %d more files\n", len(orderedKeys)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, key := range orderedKeys {
		item := itemsByKey[key]

		content := onedriveimporter.RenderContent(item)
		source := item.WebURL
		if strings.TrimSpace(source) == "" {
			source = "import:onedrive"
		}

		_, err := enqueueOneDriveContent(serverURL, content, "text", pipelineName, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		success++
	}

	fmt.Printf("OneDrive items processed: %d\n", len(orderedKeys))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d onedrive items (first error: %w)", failed, firstErr)
	}

	return nil
}

func normalizeExtensions(raw []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	for _, ext := range raw {
		ext = strings.TrimSpace(strings.ToLower(ext))
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		if ext == "." {
			return nil, fmt.Errorf("invalid --include-ext value %q", ext)
		}
		out[ext] = struct{}{}
	}
	return out, nil
}

func parseDriveFolderScopes(raw []string) ([]driveFolderScope, error) {
	out := make([]driveFolderScope, 0, len(raw))
	for _, ref := range raw {
		driveID, folderPath, err := parseDriveFolderRef(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, driveFolderScope{DriveID: driveID, Path: folderPath})
	}
	return out, nil
}

func parseItemScopes(raw []string) ([]itemScope, error) {
	out := make([]itemScope, 0, len(raw))
	for _, ref := range raw {
		driveID, itemID, err := parseItemRef(ref)
		if err != nil {
			return nil, err
		}
		out = append(out, itemScope{DriveID: driveID, ItemID: itemID})
	}
	return out, nil
}

func parseDriveFolderRef(ref string) (driveID string, folderPath string, err error) {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx == len(ref)-1 {
		return "", "", fmt.Errorf("invalid --drive-folder value %q (expected <drive-id>:/path)", ref)
	}

	driveID = strings.TrimSpace(ref[:idx])
	folderPath = strings.TrimSpace(ref[idx+1:])
	if driveID == "" || folderPath == "" {
		return "", "", fmt.Errorf("invalid --drive-folder value %q (expected <drive-id>:/path)", ref)
	}
	if !strings.HasPrefix(folderPath, "/") {
		folderPath = "/" + folderPath
	}
	return driveID, folderPath, nil
}

func parseItemRef(ref string) (driveID string, itemID string, err error) {
	ref = strings.TrimSpace(ref)
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid --item-ref value %q (expected <drive-id>/<item-id>)", ref)
	}

	driveID = strings.TrimSpace(parts[0])
	itemID = strings.TrimSpace(parts[1])
	if driveID == "" || itemID == "" {
		return "", "", fmt.Errorf("invalid --item-ref value %q (expected <drive-id>/<item-id>)", ref)
	}
	return driveID, itemID, nil
}
