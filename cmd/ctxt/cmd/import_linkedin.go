package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	linkedinimporter "github.com/ideacrafterslabs/ctxt/internal/importer/linkedin"
	"github.com/spf13/cobra"
)

var importLinkedInCmd = &cobra.Command{
	Use:   "linkedin",
	Short: "Import posts and articles from a LinkedIn data export",
	Long: `Import posts and articles from a LinkedIn data export package into ctxt.

To obtain your LinkedIn data export:
  1. Go to Settings > Data Privacy > Get a copy of your data
  2. Select "Posts" and/or "Articles" then request the archive
  3. After preparation, download and extract the ZIP file
  4. Locate Posts.csv and/or Articles.csv in the extracted folder

Then run one or both of:
  ctxt import linkedin --posts /path/to/Posts.csv
  ctxt import linkedin --articles /path/to/Articles.csv

Incremental re-imports are idempotent: use --since to limit to recent content.

Examples:
  # Import all posts from the export
  ctxt import linkedin --posts ~/Downloads/linkedin-export/Posts.csv

  # Import articles and posts together
  ctxt import linkedin --posts Posts.csv --articles Articles.csv

  # Dry-run to preview what would be imported
  ctxt import linkedin --posts Posts.csv --dry-run

  # Import only content published since a given date
  ctxt import linkedin --posts Posts.csv --articles Articles.csv --since 2023-01-01`,
	RunE: runImportLinkedIn,
}

func init() {
	importCmd.AddCommand(importLinkedInCmd)

	importLinkedInCmd.Flags().String("posts", "", "path to Posts.csv from your LinkedIn data export")
	importLinkedInCmd.Flags().String("articles", "", "path to Articles.csv from your LinkedIn data export")
	importLinkedInCmd.Flags().String("since", "", "only import content published since RFC3339 or YYYY-MM-DD")
	importLinkedInCmd.Flags().Int("max-items", 0, "maximum number of items to import (0 = all)")
	importLinkedInCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importLinkedInCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	importLinkedInCmd.Flags().Bool("dry-run", false, "preview selected items without enqueueing jobs")
}

func runImportLinkedIn(cmd *cobra.Command, _ []string) error {
	postsFile, _ := cmd.Flags().GetString("posts")
	articlesFile, _ := cmd.Flags().GetString("articles")

	if strings.TrimSpace(postsFile) == "" && strings.TrimSpace(articlesFile) == "" {
		return fmt.Errorf("at least one of --posts or --articles is required")
	}

	sinceRaw, _ := cmd.Flags().GetString("since")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	// --- Posts ---
	var posts []linkedinimporter.PostRecord
	if strings.TrimSpace(postsFile) != "" {
		data, err := os.ReadFile(postsFile)
		if err != nil {
			return fmt.Errorf("read posts file: %w", err)
		}
		parsed, err := linkedinimporter.ParsePosts(string(data))
		if err != nil {
			return fmt.Errorf("parse linkedin posts: %w", err)
		}
		posts = linkedinimporter.FilterPostsSince(parsed, since)
	}

	// --- Articles ---
	var articles []linkedinimporter.ArticleRecord
	if strings.TrimSpace(articlesFile) != "" {
		data, err := os.ReadFile(articlesFile)
		if err != nil {
			return fmt.Errorf("read articles file: %w", err)
		}
		parsed, err := linkedinimporter.ParseArticles(string(data))
		if err != nil {
			return fmt.Errorf("parse linkedin articles: %w", err)
		}
		articles = linkedinimporter.FilterArticlesSince(parsed, since)
	}

	totalItems := len(posts) + len(articles)

	// Apply global max-items cap across both types.
	if maxItems > 0 && totalItems > maxItems {
		remaining := maxItems
		if remaining >= len(posts) {
			remaining -= len(posts)
			// cap articles
			if remaining < len(articles) {
				articles = articles[:remaining]
			}
		} else {
			posts = posts[:remaining]
			articles = nil
		}
		totalItems = len(posts) + len(articles)
	}

	if totalItems == 0 {
		fmt.Println("No LinkedIn items matched the selected filters.")
		return nil
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("LinkedIn items selected: %d (dry-run)\n", totalItems)
		shown := 0

		for _, p := range posts {
			if shown >= previewLimit {
				break
			}
			label := p.ShareCommentary
			if len(label) > 70 {
				label = label[:70] + "…"
			}
			if label == "" {
				label = p.SharedURL
			}
			fmt.Printf("%d. [post] %s\n", shown+1, label)
			shown++
		}

		for _, a := range articles {
			if shown >= previewLimit {
				break
			}
			fmt.Printf("%d. [article] %s\n", shown+1, a.Title)
			shown++
		}

		if totalItems > previewLimit {
			fmt.Printf("... and %d more items\n", totalItems-previewLimit)
		}
		return nil
	}

	ctx := context.Background()
	_ = ctx // context is threaded through enqueueContent via HTTP

	var (
		success  int
		skipped  int
		failed   int
		firstErr error
	)

	seenKeys := make(map[string]struct{}, totalItems)
	pipeline := pipelineName
	if pipeline == "" {
		pipeline = "text.short"
	}

	for _, p := range posts {
		dedupeKey := linkedinimporter.PostDedupeKey(p)
		if _, exists := seenKeys[dedupeKey]; exists {
			skipped++
			continue
		}
		seenKeys[dedupeKey] = struct{}{}

		content := linkedinimporter.RenderPost(p)
		source := p.SharedURL
		if source == "" {
			source = "import:linkedin"
		}

		_, err := enqueueContent(serverURL, content, "text", pipeline, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	for _, a := range articles {
		dedupeKey := linkedinimporter.ArticleDedupeKey(a)
		if _, exists := seenKeys[dedupeKey]; exists {
			skipped++
			continue
		}
		seenKeys[dedupeKey] = struct{}{}

		content := linkedinimporter.RenderArticle(a)
		source := a.URL
		if source == "" {
			source = "import:linkedin"
		}

		_, err := enqueueContent(serverURL, content, "text", pipeline, source)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("LinkedIn items processed: %d\n", totalItems)
	fmt.Printf("  Posts:    %d\n", len(posts))
	fmt.Printf("  Articles: %d\n", len(articles))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if skipped > 0 {
		fmt.Printf("Skipped (dedup): %d\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d linkedin items (first error: %w)", failed, firstErr)
	}

	return nil
}
