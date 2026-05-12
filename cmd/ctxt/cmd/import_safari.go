package cmd

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
	"github.com/spf13/cobra"
)

var importSafariCmd = &cobra.Command{
	Use:   "safari",
	Short: "Import Safari bookmarks export",
	Long: `Import bookmarks from a Safari bookmarks HTML export file.

Examples:
  # Dry-run parse and preview bookmarks
  ctxt import safari --file ./bookmarks.html --dry-run

  # Enqueue bookmark URLs for ingestion
  ctxt import safari --file ./bookmarks.html --server http://localhost:8080`,
	RunE: runImportSafari,
}

func init() {
	importCmd.AddCommand(importSafariCmd)

	importSafariCmd.Flags().String("file", "", "path to Safari bookmarks HTML export")
	importSafariCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importSafariCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	importSafariCmd.Flags().Int("max-items", 0, "maximum number of bookmarks to import (0 = all)")

	importSafariCmd.MarkFlagRequired("file")

	cliconv.WithSideEffect(importSafariCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importSafariCmd, cliconv.IdempotencyConditional)
}

func runImportSafari(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	bookmarks, err := bookmarksimporter.ParseBookmarksFile(file)
	if err != nil {
		return err
	}
	if len(bookmarks) == 0 {
		return fmt.Errorf("no bookmarks found in %s", file)
	}

	if maxItems > 0 && len(bookmarks) > maxItems {
		bookmarks = bookmarks[:maxItems]
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Safari bookmarks parsed: %d (dry-run)\n", len(bookmarks))

		preview := len(bookmarks)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			b := bookmarks[i]
			if b.FolderPath == "" {
				fmt.Printf("%d. %s -> %s\n", i+1, b.Title, b.URL)
			} else {
				fmt.Printf("%d. [%s] %s -> %s\n", i+1, b.FolderPath, b.Title, b.URL)
			}
		}

		if len(bookmarks) > previewLimit {
			fmt.Printf("... and %d more bookmarks\n", len(bookmarks)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, b := range bookmarks {
		_, err := enqueueBookmark(serverURL, b.URL, pipelineName, "import:safari")
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("Safari bookmarks processed: %d\n", len(bookmarks))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d bookmarks (first error: %w)", failed, firstErr)
	}

	return nil
}
