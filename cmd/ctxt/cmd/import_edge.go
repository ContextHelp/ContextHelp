package cmd

import (
	"fmt"
	"strings"

	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
	"github.com/spf13/cobra"
)

var importEdgeCmd = &cobra.Command{
	Use:   "edge",
	Short: "Import Edge bookmarks export",
	Long: `Import bookmarks from a Microsoft Edge bookmarks HTML export file.

Examples:
  # Dry-run parse and preview bookmarks
  ctxt import edge --file ./bookmarks.html --dry-run

  # Enqueue bookmark URLs for ingestion
  ctxt import edge --file ./bookmarks.html --server http://localhost:8080`,
	RunE: runImportEdge,
}

func init() {
	importCmd.AddCommand(importEdgeCmd)

	importEdgeCmd.Flags().String("file", "", "path to Edge bookmarks HTML export")
	importEdgeCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importEdgeCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	importEdgeCmd.Flags().Int("max-items", 0, "maximum number of bookmarks to import (0 = all)")
	importEdgeCmd.Flags().Bool("dry-run", false, "parse and preview without enqueueing jobs")

	importEdgeCmd.MarkFlagRequired("file")
}

func runImportEdge(cmd *cobra.Command, args []string) error {
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
		fmt.Printf("Edge bookmarks parsed: %d (dry-run)\n", len(bookmarks))

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
		_, err := enqueueBookmark(serverURL, b.URL, pipelineName, "import:edge")
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("Edge bookmarks processed: %d\n", len(bookmarks))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d bookmarks (first error: %w)", failed, firstErr)
	}

	return nil
}
