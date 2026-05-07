package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	pinboardimporter "github.com/ideacrafterslabs/ctxt/internal/importer/pinboard"
	"github.com/spf13/cobra"
)

const pinboardTokenEnv = "PINBOARD_TOKEN"

type pinboardClient interface {
	FetchPosts(ctx context.Context, opts pinboardimporter.FetchOptions) ([]pinboardimporter.Bookmark, error)
}

var newPinboardClient = func(token, baseURL string) pinboardClient {
	return pinboardimporter.NewClient(nil, baseURL, token)
}

var enqueuePinboardItem = enqueueImportItem

var importPinboardCmd = &cobra.Command{
	Use:   "pinboard",
	Short: "Import Pinboard bookmarks",
	Long: `Import Pinboard bookmarks from API and/or export file.

What you must provide:
  1) A source:
     - API token via --token or PINBOARD_TOKEN, and/or
     - local export via --file <pinboard.json>
  2) A reachable dpkms server (--server) unless using --dry-run

How import scope is selective:
  - --since RFC3339 or YYYY-MM-DD (saved time floor)
  - --tagged can be repeated; all listed tags must match
  - --max-items caps total selected bookmarks
  - you can combine API + file sources in one run

Examples:
  # Dry-run from API bookmarks saved since 2026-01-01 with "go" tag
  ctxt import pinboard --token $PINBOARD_TOKEN --since 2026-01-01 --tagged go --dry-run

  # Import from local export file
  ctxt import pinboard --file ./pinboard.json --server http://localhost:8080

  # Merge API + file, then enqueue up to 500 bookmarks
  ctxt import pinboard --token $PINBOARD_TOKEN --file ./pinboard.json --max-items 500`,
	RunE: runImportPinboard,
}

func init() {
	importCmd.AddCommand(importPinboardCmd)

	importPinboardCmd.Flags().String("token", "", "Pinboard API token (or set PINBOARD_TOKEN)")
	importPinboardCmd.Flags().String("file", "", "path to Pinboard export JSON file")
	importPinboardCmd.Flags().String("pinboard-base-url", "", "Pinboard API base URL override (tests/dev only)")
	importPinboardCmd.Flags().String("since", "", "import bookmarks saved on/after this time (RFC3339 or YYYY-MM-DD)")
	importPinboardCmd.Flags().StringSlice("tagged", nil, "require bookmarks to include all specified tags")
	importPinboardCmd.Flags().Int("max-items", 0, "maximum bookmarks to import (0 = all)")
	importPinboardCmd.Flags().Bool("dry-run", false, "preview matched bookmarks without enqueueing jobs")
	importPinboardCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importPinboardCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
}

func runImportPinboard(cmd *cobra.Command, args []string) error {
	token, _ := cmd.Flags().GetString("token")
	file, _ := cmd.Flags().GetString("file")
	baseURL, _ := cmd.Flags().GetString("pinboard-base-url")
	sinceRaw, _ := cmd.Flags().GetString("since")
	tags, _ := cmd.Flags().GetStringSlice("tagged")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")

	token = strings.TrimSpace(token)
	if token == "" {
		token = strings.TrimSpace(os.Getenv(pinboardTokenEnv))
	}

	file = strings.TrimSpace(file)
	if token == "" && file == "" {
		return fmt.Errorf("pinboard source is required: use --token/%s and/or --file", pinboardTokenEnv)
	}

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	since, err := parsePinboardSince(sinceRaw)
	if err != nil {
		return err
	}

	normalizedTags := normalizePinboardTags(tags)

	collected := make([]pinboardimporter.Bookmark, 0)
	scanned := 0

	if file != "" {
		fromFile, err := pinboardimporter.ParseExportFile(file)
		if err != nil {
			return err
		}
		scanned += len(fromFile)
		collected = append(collected, fromFile...)
	}

	if token != "" {
		client := newPinboardClient(token, baseURL)
		fromAPI, err := client.FetchPosts(context.Background(), pinboardimporter.FetchOptions{
			Since:    since,
			Tags:     normalizedTags,
			MaxItems: 0,
		})
		if err != nil {
			return err
		}
		scanned += len(fromAPI)
		collected = append(collected, fromAPI...)
	}

	filtered := pinboardimporter.FilterBookmarks(collected, since, normalizedTags, 0)
	selected := dedupePinboardBookmarks(filtered)
	if maxItems > 0 && len(selected) > maxItems {
		selected = selected[:maxItems]
	}

	if len(selected) == 0 {
		return fmt.Errorf("no Pinboard bookmarks matched the selected scope")
	}

	fmt.Printf("Pinboard scope: %s\n", describePinboardScope(file != "", token != "", since, normalizedTags, maxItems))

	skipped := scanned - len(selected)
	if skipped < 0 {
		skipped = 0
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Pinboard bookmarks selected: %d (dry-run)\n", len(selected))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(selected)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			bookmark := selected[i]
			tags := "-"
			if len(bookmark.Tags) > 0 {
				tags = strings.Join(bookmark.Tags, ",")
			}
			fmt.Printf("%d. [%s] %s -> %s\n", i+1, tags, bookmark.Title, bookmark.URL)
		}
		if len(selected) > previewLimit {
			fmt.Printf("... and %d more bookmarks\n", len(selected)-previewLimit)
		}
		return nil
	}

	var (
		imported int
		failed   int
		firstErr error
	)

	for _, bookmark := range selected {
		payload := pinboardimporter.RenderContent(bookmark)
		if strings.TrimSpace(payload) == "" {
			continue
		}

		source := strings.TrimSpace(bookmark.URL)
		if source == "" {
			source = "import:pinboard"
		}

		_, err := enqueuePinboardItem(serverURL, payload, "text", pipelineName, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		imported++
	}

	fmt.Printf("Pinboard bookmarks scanned: %d\n", scanned)
	fmt.Printf("Selected: %d\n", len(selected))
	fmt.Printf("Imported: %d\n", imported)
	fmt.Printf("Skipped: %d\n", skipped)
	if failed > 0 {
		fmt.Printf("Failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("pinboard import completed with errors (first error: %w)", firstErr)
	}

	return nil
}

func parsePinboardSince(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		return &parsed, nil
	}

	parsed, err = time.Parse("2006-01-02", raw)
	if err == nil {
		return &parsed, nil
	}

	return nil, fmt.Errorf("invalid --since value %q: expected RFC3339 or YYYY-MM-DD", raw)
}

func normalizePinboardTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	seen := map[string]struct{}{}

	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}

	return normalized
}

func dedupePinboardBookmarks(bookmarks []pinboardimporter.Bookmark) []pinboardimporter.Bookmark {
	out := make([]pinboardimporter.Bookmark, 0, len(bookmarks))
	seen := make(map[string]struct{}, len(bookmarks))

	for _, bookmark := range bookmarks {
		key := strings.TrimSpace(bookmark.Hash)
		if key == "" {
			key = strings.TrimSpace(bookmark.URL)
		}
		if key == "" {
			key = strings.TrimSpace(bookmark.Title)
		}
		if key == "" {
			continue
		}

		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, bookmark)
	}

	return out
}

func describePinboardScope(withFile bool, withAPI bool, since *time.Time, tags []string, maxItems int) string {
	parts := make([]string, 0, 5)

	switch {
	case withFile && withAPI:
		parts = append(parts, "source=file+api")
	case withFile:
		parts = append(parts, "source=file")
	case withAPI:
		parts = append(parts, "source=api")
	}

	if since != nil {
		parts = append(parts, "since="+since.UTC().Format(time.RFC3339))
	}
	if len(tags) > 0 {
		parts = append(parts, "tags="+strings.Join(tags, ","))
	}
	if maxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", maxItems))
	}

	if len(parts) == 0 {
		return "all bookmarks"
	}
	return strings.Join(parts, "; ")
}
