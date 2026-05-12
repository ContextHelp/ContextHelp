package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	raindropimporter "github.com/ideacrafterslabs/ctxt/internal/importer/raindrop"
	"github.com/spf13/cobra"
)

const raindropTokenEnv = "RAINDROP_TOKEN"

var importRaindropCmd = &cobra.Command{
	Use:   "raindrop",
	Short: "Import bookmarks from Raindrop.io",
	Long: `Import bookmarks from Raindrop.io collections into ctxt.

Required from user:
  1) Raindrop API token:
     --token <token> or RAINDROP_TOKEN env var

  2) At least one scope selector:
     --collection-id <id> (repeatable)
     --all  (imports all bookmarks via collection 0)

Selective controls:
  --since <RFC3339|YYYY-MM-DD>   only include updated items
  --tagged <tag>                 exact tag filter (case-insensitive)
  --query <text>                 text filter across title/url/note/excerpt/tags
  --max-items <n>                cap selected items

Examples:
  # Dry-run everything
  ctxt import raindrop --all --dry-run

  # Import selected collections and filter to recent work links
  ctxt import raindrop --collection-id 42 --collection-id 77 --tagged work --since 2026-01-01`,
	RunE: runImportRaindrop,
}

func init() {
	importCmd.AddCommand(importRaindropCmd)

	importRaindropCmd.Flags().String("token", "", "Raindrop API token (or RAINDROP_TOKEN env var)")
	importRaindropCmd.Flags().StringSlice("collection-id", nil, "Raindrop collection ID to import from (repeatable)")
	importRaindropCmd.Flags().Bool("all", false, "import from all collections")
	importRaindropCmd.Flags().String("query", "", "text filter over title/url/excerpt/note/tags/highlights")
	importRaindropCmd.Flags().String("tagged", "", "exact tag filter (case-insensitive)")
	importRaindropCmd.Flags().String("since", "", "only import items updated since RFC3339 or YYYY-MM-DD")
	importRaindropCmd.Flags().Int("max-items", 0, "maximum number of items to import (0 = all)")
	importRaindropCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importRaindropCmd.Flags().String("pipeline", "import.raindrop", "pipeline override for enqueued jobs")

	// For testing and self-hosted proxies.
	importRaindropCmd.Flags().String("raindrop-base-url", "", "override Raindrop API base URL")
	_ = importRaindropCmd.Flags().MarkHidden("raindrop-base-url")
}

func runImportRaindrop(cmd *cobra.Command, args []string) error {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token = os.Getenv(raindropTokenEnv)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("raindrop token is required (use --token or %s)", raindropTokenEnv)
	}

	allCollections, _ := cmd.Flags().GetBool("all")
	collectionIDsRaw, _ := cmd.Flags().GetStringSlice("collection-id")
	query, _ := cmd.Flags().GetString("query")
	tag, _ := cmd.Flags().GetString("tagged")
	sinceRaw, _ := cmd.Flags().GetString("since")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	baseURL, _ := cmd.Flags().GetString("raindrop-base-url")

	if !allCollections && len(collectionIDsRaw) == 0 {
		return fmt.Errorf("provide at least one scope: --collection-id or --all")
	}

	collectionIDs, err := parseRaindropCollectionIDs(collectionIDsRaw)
	if err != nil {
		return err
	}
	if allCollections {
		collectionIDs = append(collectionIDs, 0)
	}
	collectionIDs = dedupeCollectionIDs(collectionIDs)

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	client := raindropimporter.NewClient(nil, baseURL, token)
	opts := raindropimporter.ListOptions{
		CollectionIDs: collectionIDs,
		Query:         query,
		Tag:           tag,
		Since:         since,
		MaxItems:      maxItems,
	}

	ctx := context.Background()
	items, err := client.ListItems(ctx, opts)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no raindrop items matched the selected scope")
	}

	collectionPathByID := make(map[int64]string)
	collections, err := client.ListCollections(ctx)
	if err == nil {
		collectionPathByID = raindropimporter.BuildCollectionPathMap(collections)
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Raindrop items selected: %d (dry-run)\n", len(items))

		preview := len(items)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			item := items[i]
			collection := collectionPathByID[item.CollectionID]
			if collection == "" {
				collection = fmt.Sprintf("collection:%d", item.CollectionID)
			}
			if strings.TrimSpace(item.Link) == "" {
				fmt.Printf("%d. [%s] %s (id=%d)\n", i+1, collection, item.Title, item.ID)
			} else {
				fmt.Printf("%d. [%s] %s -> %s\n", i+1, collection, item.Title, item.Link)
			}
		}
		if len(items) > previewLimit {
			fmt.Printf("... and %d more items\n", len(items)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, item := range items {
		collectionPath := collectionPathByID[item.CollectionID]
		content := raindropimporter.RenderContent(item, collectionPath)

		source := strings.TrimSpace(item.Link)
		if source == "" {
			source = "import:raindrop"
		}

		_, err := enqueueContent(serverURL, content, "text", pipelineName, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("Raindrop items processed: %d\n", len(items))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d raindrop items (first error: %w)", failed, firstErr)
	}

	return nil
}

func parseRaindropCollectionIDs(values []string) ([]int64, error) {
	out := make([]int64, 0, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid --collection-id value %q: expected integer", raw)
		}
		out = append(out, id)
	}
	return out, nil
}

func dedupeCollectionIDs(values []int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	out := make([]int64, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
