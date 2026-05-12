package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	notionimporter "github.com/ideacrafterslabs/ctxt/internal/importer/notion"
	"github.com/spf13/cobra"
)

var importNotionCmd = &cobra.Command{
	Use:   "notion",
	Short: "Import pages from Notion API",
	Long: `Import content directly from Notion using an integration token.

At least one scope selector is required:
  - --page-id <id> (repeatable)
  - --database-id <id> (repeatable)
  - --all-shared (all shared pages, optionally filtered by --query)`,
	RunE: runImportNotion,
}

func init() {
	importCmd.AddCommand(importNotionCmd)

	importNotionCmd.Flags().String("token", "", "Notion integration token (or use NOTION_TOKEN env var)")
	importNotionCmd.Flags().StringSlice("page-id", nil, "Notion page ID to import (repeatable)")
	importNotionCmd.Flags().StringSlice("database-id", nil, "Notion database ID to import (repeatable)")
	importNotionCmd.Flags().Bool("all-shared", false, "import all pages shared with the integration")
	importNotionCmd.Flags().String("query", "", "optional query for all-shared search")
	importNotionCmd.Flags().String("since", "", "only import pages edited since RFC3339 timestamp")
	importNotionCmd.Flags().Int("max-items", 0, "maximum pages to import (0 = all)")

	importNotionCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importNotionCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	// For testing and self-hosted proxies.
	importNotionCmd.Flags().String("notion-base-url", "", "override Notion API base URL")
	_ = importNotionCmd.Flags().MarkHidden("notion-base-url")

	cliconv.WithSideEffect(importNotionCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importNotionCmd, cliconv.IdempotencyConditional)
}

func runImportNotion(cmd *cobra.Command, args []string) error {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token = os.Getenv("NOTION_TOKEN")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("notion token is required (use --token or NOTION_TOKEN)")
	}

	pageIDs, _ := cmd.Flags().GetStringSlice("page-id")
	databaseIDs, _ := cmd.Flags().GetStringSlice("database-id")
	allShared, _ := cmd.Flags().GetBool("all-shared")
	query, _ := cmd.Flags().GetString("query")
	sinceRaw, _ := cmd.Flags().GetString("since")
	maxItems, _ := cmd.Flags().GetInt("max-items")

	if len(pageIDs) == 0 && len(databaseIDs) == 0 && !allShared {
		return fmt.Errorf("provide at least one scope: --page-id, --database-id, or --all-shared")
	}

	var since *time.Time
	if strings.TrimSpace(sinceRaw) != "" {
		parsed, err := time.Parse(time.RFC3339, sinceRaw)
		if err != nil {
			return fmt.Errorf("invalid --since timestamp: %w", err)
		}
		since = &parsed
	}

	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	notionBaseURL, _ := cmd.Flags().GetString("notion-base-url")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	client := notionimporter.NewClient(nil, notionBaseURL, token)
	ctx := context.Background()

	pagesByID := make(map[string]notionimporter.Page)
	orderedIDs := make([]string, 0)
	addPage := func(p notionimporter.Page) {
		if p.ID == "" {
			return
		}
		if _, exists := pagesByID[p.ID]; exists {
			return
		}
		pagesByID[p.ID] = p
		orderedIDs = append(orderedIDs, p.ID)
	}
	remaining := func() int {
		if maxItems <= 0 {
			return 0
		}
		return maxItems - len(orderedIDs)
	}

	var (
		scanned int
		skipped int
	)

	for _, pageID := range pageIDs {
		if maxItems > 0 && len(orderedIDs) >= maxItems {
			break
		}
		p, err := client.GetPage(ctx, pageID)
		if err != nil {
			return fmt.Errorf("fetch page %s: %w", pageID, err)
		}
		scanned++
		if since != nil && !p.LastEditedTime.IsZero() && p.LastEditedTime.Before(*since) {
			skipped++
			continue
		}
		addPage(p)
	}

	for _, dbID := range databaseIDs {
		if maxItems > 0 && len(orderedIDs) >= maxItems {
			break
		}
		limit := remaining()
		pages, err := client.QueryDatabasePages(ctx, dbID, since, limit)
		if err != nil {
			return fmt.Errorf("query database %s: %w", dbID, err)
		}
		scanned += len(pages)
		for _, p := range pages {
			addPage(p)
			if maxItems > 0 && len(orderedIDs) >= maxItems {
				break
			}
		}
	}

	if allShared && (maxItems <= 0 || len(orderedIDs) < maxItems) {
		limit := remaining()
		pages, err := client.SearchPages(ctx, query, since, limit)
		if err != nil {
			return fmt.Errorf("search shared pages: %w", err)
		}
		scanned += len(pages)
		for _, p := range pages {
			addPage(p)
			if maxItems > 0 && len(orderedIDs) >= maxItems {
				break
			}
		}
	}

	if len(orderedIDs) == 0 {
		return fmt.Errorf("no notion pages selected for import")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Notion pages selected: %d (dry-run)\n", len(orderedIDs))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(orderedIDs)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			p := pagesByID[orderedIDs[i]]
			if p.URL == "" {
				fmt.Printf("%d. %s (%s)\n", i+1, p.Title, p.ID)
			} else {
				fmt.Printf("%d. %s (%s) -> %s\n", i+1, p.Title, p.ID, p.URL)
			}
		}
		if len(orderedIDs) > previewLimit {
			fmt.Printf("... and %d more pages\n", len(orderedIDs)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, pageID := range orderedIDs {
		p := pagesByID[pageID]

		body, err := client.PageContent(ctx, p.ID)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("fetch page content %s: %w", p.ID, err)
			}
			continue
		}

		content := notionimporter.RenderContent(p, body)
		source := p.URL
		if source == "" {
			source = "import:notion"
		}

		_, err = enqueueContent(serverURL, content, "text", pipelineName, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		success++
	}

	fmt.Printf("Notion pages processed: %d\n", len(orderedIDs))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d notion pages (first error: %w)", failed, firstErr)
	}

	return nil
}
