package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	logseqImporter "github.com/ideacrafterslabs/ctxt/internal/importer/logseq"
	"github.com/spf13/cobra"
)

var importLogseqCmd = &cobra.Command{
	Use:   "logseq",
	Short: "Import a Logseq graph",
	Long: `Import pages and journals from a local Logseq graph directory.

Required from user:
  1) --graph path to your Logseq graph directory

Selective import scope:
  --since RFC3339 or YYYY-MM-DD     only import pages modified on or after this time
  --kind journal|page               only import this kind (default: all)
  --max-items N                     cap selected pages
  --dry-run                         preview selected pages without importing

Examples:
  # Dry-run and inspect selected pages
  ctxt import logseq --graph ~/Documents/MyGraph --dry-run

  # Import only journals modified since a date
  ctxt import logseq --graph ~/Documents/MyGraph --since 2026-01-01 --kind journal

  # Import with a custom server
  ctxt import logseq --graph ~/Documents/MyGraph --server http://localhost:8080`,
	RunE: runImportLogseq,
}

func init() {
	importCmd.AddCommand(importLogseqCmd)

	importLogseqCmd.Flags().String("graph", "", "path to Logseq graph directory")
	importLogseqCmd.Flags().String("since", "", "only import pages modified since this time (RFC3339 or YYYY-MM-DD)")
	importLogseqCmd.Flags().String("kind", "", "filter by page kind: journal|page (default: all)")
	importLogseqCmd.Flags().Int("max-items", 0, "maximum pages to import (0 = all)")
	importLogseqCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importLogseqCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	importLogseqCmd.MarkFlagRequired("graph")

	cliconv.WithSideEffect(importLogseqCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importLogseqCmd, cliconv.IdempotencyConditional)
}

func runImportLogseq(cmd *cobra.Command, args []string) error {
	graphDir, _ := cmd.Flags().GetString("graph")
	sinceRaw, _ := cmd.Flags().GetString("since")
	kindFilter, _ := cmd.Flags().GetString("kind")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	// Validate kind filter.
	kindFilter = strings.ToLower(strings.TrimSpace(kindFilter))
	if kindFilter != "" && kindFilter != "journal" && kindFilter != "page" {
		return fmt.Errorf("--kind must be 'journal' or 'page', got %q", kindFilter)
	}

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	pages, err := logseqImporter.WalkGraph(graphDir)
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		return fmt.Errorf("no markdown pages found in graph %s", graphDir)
	}

	fmt.Printf("Logseq scope: %s\n", describeLogseqScope(since, kindFilter, maxItems))

	scanned := len(pages)
	skipped := 0
	selected := make([]logseqImporter.Page, 0, len(pages))

	for _, p := range pages {
		if since != nil && !p.ModTime.IsZero() && p.ModTime.Before(*since) {
			skipped++
			continue
		}
		if kindFilter != "" && string(p.Kind) != kindFilter {
			skipped++
			continue
		}
		selected = append(selected, p)
	}

	if maxItems > 0 && len(selected) > maxItems {
		skipped += len(selected) - maxItems
		selected = selected[:maxItems]
	}

	if len(selected) == 0 {
		return fmt.Errorf("no Logseq pages matched the selected scope")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Logseq pages selected: %d (dry-run)\n", len(selected))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(selected)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			p := selected[i]
			modLabel := "-"
			if !p.ModTime.IsZero() {
				modLabel = p.ModTime.UTC().Format(time.RFC3339)
			}
			tagLabel := "-"
			if len(p.Tags) > 0 {
				tagLabel = strings.Join(p.Tags, ",")
			}
			fmt.Printf("%d. %s (id=%s, kind=%s, path=%s, modified=%s, tags=%s)\n",
				i+1, p.Title, p.ExternalID, p.Kind, p.RelPath, modLabel, tagLabel)
		}
		if len(selected) > previewLimit {
			fmt.Printf("... and %d more pages\n", len(selected)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, p := range selected {
		content := logseqImporter.RenderContent(p)
		source := "import:logseq:" + p.RelPath

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

	fmt.Printf("Logseq pages processed: %d\n", len(selected))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d Logseq pages (first error: %w)", failed, firstErr)
	}

	return nil
}

func describeLogseqScope(since *time.Time, kind string, maxItems int) string {
	parts := make([]string, 0, 3)
	if since != nil {
		parts = append(parts, "since="+since.UTC().Format(time.RFC3339))
	}
	if kind != "" {
		parts = append(parts, "kind="+kind)
	}
	if maxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", maxItems))
	}
	if len(parts) == 0 {
		return "all pages from graph"
	}
	return strings.Join(parts, "; ")
}
