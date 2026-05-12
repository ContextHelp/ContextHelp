package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	twitterimporter "github.com/ideacrafterslabs/ctxt/internal/importer/twitter"
	"github.com/spf13/cobra"
)

var importTwitterCmd = &cobra.Command{
	Use:   "twitter",
	Short: "Import tweets from a Twitter/X archive export",
	Long: `Import tweets from a Twitter/X archive export file into ctxt.

To obtain your Twitter/X archive:
  1. Go to Settings > Your Account > Download an archive of your data
  2. After preparation, download and extract the ZIP file
  3. Locate tweets.js (usually at data/tweets.js inside the archive)

Then run:
  ctxt import twitter --file /path/to/tweets.js

Incremental re-imports are idempotent: use --since to limit to recent tweets.

Examples:
  # Import all tweets from an archive
  ctxt import twitter --file ~/Downloads/twitter-archive/data/tweets.js

  # Dry-run to preview what would be imported
  ctxt import twitter --file tweets.js --dry-run

  # Import only tweets since a given date
  ctxt import twitter --file tweets.js --since 2023-01-01

  # Cap the number of tweets imported
  ctxt import twitter --file tweets.js --max-items 100`,
	RunE: runImportTwitter,
}

func init() {
	importCmd.AddCommand(importTwitterCmd)

	importTwitterCmd.Flags().String("file", "", "path to tweets.js from your Twitter/X archive (required)")
	importTwitterCmd.Flags().String("username", "", "your Twitter/X username (used for canonical tweet URLs)")
	importTwitterCmd.Flags().String("since", "", "only import tweets created since RFC3339 or YYYY-MM-DD")
	importTwitterCmd.Flags().Int("max-items", 0, "maximum number of tweets to import (0 = all)")
	importTwitterCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importTwitterCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	cliconv.WithSideEffect(importTwitterCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importTwitterCmd, cliconv.IdempotencyConditional)

	cliconv.WithExamples(importTwitterCmd, []cliconv.Example{
		{
			Title:   "Import from default source",
			Command: "ctxt import twitter --file ./tweets.js",
		},
		{
			Title:   "Dry-run preview",
			Command: "ctxt import twitter --file ./tweets.js --confirm=no --dry-run",
		},
	})
	cliconv.WithNextSteps(importTwitterCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "verify imported items"},
		{When: "on success", Suggest: "ctxt find <keyword>", Reason: "search the newly-imported data"},
	})
}

func runImportTwitter(cmd *cobra.Command, _ []string) error {
	filePath, _ := cmd.Flags().GetString("file")
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("--file is required (path to tweets.js from your Twitter/X archive)")
	}

	username, _ := cmd.Flags().GetString("username")
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

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read archive file: %w", err)
	}

	tweets, err := twitterimporter.ParseArchive(string(data))
	if err != nil {
		return fmt.Errorf("parse twitter archive: %w", err)
	}

	// Apply time filter.
	tweets = twitterimporter.FilterSince(tweets, since)

	// Apply max-items cap.
	if maxItems > 0 && len(tweets) > maxItems {
		tweets = tweets[:maxItems]
	}

	if len(tweets) == 0 {
		fmt.Println("No tweets matched the selected filters.")
		return nil
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Twitter/X tweets selected: %d (dry-run)\n", len(tweets))

		preview := len(tweets)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			tw := tweets[i]
			preview80 := tw.FullText
			if len(preview80) > 80 {
				preview80 = preview80[:80] + "…"
			}
			fmt.Printf("%d. [%s] %s\n", i+1, tw.ID, preview80)
		}
		if len(tweets) > previewLimit {
			fmt.Printf("... and %d more tweets\n", len(tweets)-previewLimit)
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

	seenKeys := make(map[string]struct{}, len(tweets))

	for _, tw := range tweets {
		dedupeKey := twitterimporter.DedupeKey(tw)
		if _, exists := seenKeys[dedupeKey]; exists {
			skipped++
			continue
		}
		seenKeys[dedupeKey] = struct{}{}

		content := twitterimporter.RenderContent(tw)
		source := twitterimporter.TweetURL(username, tw.ID)

		pipeline := pipelineName
		if pipeline == "" {
			pipeline = "text.short"
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

	fmt.Printf("Twitter/X archive: %s\n", filePath)
	fmt.Printf("Tweets processed:  %d\n", len(tweets))
	fmt.Printf("Jobs enqueued:     %d\n", success)
	if skipped > 0 {
		fmt.Printf("Skipped (dedup):   %d\n", skipped)
	}
	if failed > 0 {
		fmt.Printf("Jobs failed:       %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d tweets (first error: %w)", failed, firstErr)
	}

	return nil
}
