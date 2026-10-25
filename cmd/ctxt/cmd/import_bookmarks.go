package cmd

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	bookmarksimporter "github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
	"github.com/spf13/cobra"
)

// bookmarkBrowser names one browser whose bookmarks HTML export
// `ctxt import <name>` reads. The browser commands differ only in these
// strings; the export format is the same Netscape bookmark file.
type bookmarkBrowser struct {
	name     string // subcommand and job source suffix, e.g. "edge"
	label    string // display name in output and short help, e.g. "Edge"
	longName string // name in the long help, e.g. "Microsoft Edge"
}

// newBookmarkImportCmd builds `ctxt import <browser>`.
func newBookmarkImportCmd(b bookmarkBrowser) *cobra.Command {
	cmd := &cobra.Command{
		Use:   b.name,
		Short: fmt.Sprintf("Import %s bookmarks export", b.label),
		Long: fmt.Sprintf(`Import bookmarks from a %s bookmarks HTML export file.

Examples:
  # Dry-run parse and preview bookmarks
  ctxt import %s --file ./bookmarks.html --dry-run

  # Enqueue bookmark URLs for ingestion
  ctxt import %s --file ./bookmarks.html`, b.longName, b.name, b.name),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runBookmarkImport(cmd, b)
		},
	}

	cmd.Flags().String("file", "", fmt.Sprintf("path to %s bookmarks HTML export", b.label))
	cmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")
	cmd.Flags().Int("max-items", 0, "maximum number of bookmarks to import (0 = all)")

	markFlagsRequired(cmd, "file")

	markImportCmd(cmd, fmt.Sprintf("ctxt import %s --file ./bookmarks.html", b.name))
	return cmd
}

func runBookmarkImport(cmd *cobra.Command, b bookmarkBrowser) error {
	file, _ := cmd.Flags().GetString("file")
	dc, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

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
		fmt.Printf("%s bookmarks parsed: %d (dry-run)\n", b.label, len(bookmarks))

		preview := len(bookmarks)
		if preview > previewLimit {
			preview = previewLimit
		}

		for i := 0; i < preview; i++ {
			bm := bookmarks[i]
			if bm.FolderPath == "" {
				fmt.Printf("%d. %s -> %s\n", i+1, bm.Title, bm.URL)
			} else {
				fmt.Printf("%d. [%s] %s -> %s\n", i+1, bm.FolderPath, bm.Title, bm.URL)
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

	for _, bm := range bookmarks {
		_, err := enqueueBookmark(dc, bm.URL, pipelineName, "import:"+b.name)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		success++
	}

	fmt.Printf("%s bookmarks processed: %d\n", b.label, len(bookmarks))
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d bookmarks (first error: %w)", failed, firstErr)
	}

	return nil
}

// Object types the dpkms enqueue and analyze endpoints accept for
// content ctxt sends.
const (
	objectTypeText = "text"
	objectTypeURL  = "url"
)

// pipelineTextShort is the dpkms pipeline for short text items: posts,
// tweets, notes.
const pipelineTextShort = "text.short"

type enqueueRequest struct {
	Content  string `json:"content"`
	Type     string `json:"type"`
	Pipeline string `json:"pipeline,omitempty"`
	Source   string `json:"source,omitempty"`
}

type enqueueResponse struct {
	JobID string `json:"job_id"`
}

func enqueueBookmark(dc *dpkmsclient.Client, url, pipelineName, source string) (string, error) {
	return enqueueContent(dc, url, objectTypeURL, pipelineName, source)
}

// enqueueContent enqueues one item on the dpkms dc talks to and returns
// its job ID.
func enqueueContent(dc *dpkmsclient.Client, content, contentType, pipelineName, source string) (string, error) {
	payload := enqueueRequest{
		Content:  content,
		Type:     contentType,
		Pipeline: pipelineName,
		Source:   source,
	}
	var out enqueueResponse
	if err := dc.Post(context.Background(), "/api/v1/pipelines/enqueue", payload, &out); err != nil {
		return "", fmt.Errorf("enqueue: %w", err)
	}
	return out.JobID, nil
}
