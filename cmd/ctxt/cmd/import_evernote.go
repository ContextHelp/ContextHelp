package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	evernoteimporter "github.com/ideacrafterslabs/ctxt/internal/importer/evernote"
	"github.com/spf13/cobra"
)

var importEvernoteCmd = &cobra.Command{
	Use:   "evernote",
	Short: "Import Evernote ENEX/HTML export files",
	Long: `Import notes from local Evernote export files.

Required from user:
  1) --file path to an Evernote export file (.enex or .html)

Selective import scope:
  --since RFC3339 or YYYY-MM-DD     only notes updated/created on or after this time
  --tagged <name> (repeatable)      include only notes containing at least one tag
  --max-items N                     cap selected notes

Examples:
  # Dry-run and inspect selected notes
  ctxt import evernote --file ./notes.enex --dry-run

  # Import only recent work-tagged notes
  ctxt import evernote --file ./notes.enex --since 2026-01-01 --tagged work --server http://localhost:8080`,
	RunE: runImportEvernote,
}

func init() {
	importCmd.AddCommand(importEvernoteCmd)

	importEvernoteCmd.Flags().String("file", "", "path to Evernote export file (.enex or .html)")
	importEvernoteCmd.Flags().String("since", "", "only import notes modified since this time (RFC3339 or YYYY-MM-DD)")
	importEvernoteCmd.Flags().StringSlice("tagged", nil, "only include notes with at least one of these tags (repeatable)")
	importEvernoteCmd.Flags().Int("max-items", 0, "maximum notes to import (0 = all)")
	importEvernoteCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importEvernoteCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	importEvernoteCmd.MarkFlagRequired("file")

	cliconv.WithSideEffect(importEvernoteCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importEvernoteCmd, cliconv.IdempotencyConditional)
}

func runImportEvernote(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	sinceRaw, _ := cmd.Flags().GetString("since")
	filterTags, _ := cmd.Flags().GetStringSlice("tagged")
	maxItems, _ := cmd.Flags().GetInt("max-items")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	since, err := parseSinceValue(sinceRaw)
	if err != nil {
		return err
	}

	notes, err := evernoteimporter.ParseExportFile(file)
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return fmt.Errorf("no evernote notes found in %s", file)
	}

	tagFilters := normalizeTagFilters(filterTags)
	fmt.Printf("Evernote scope: %s\n", describeEvernoteScope(since, tagFilters, maxItems))

	scanned := len(notes)
	skipped := 0
	selected := make([]evernoteimporter.Note, 0, len(notes))

	for _, n := range notes {
		if since != nil {
			ts := noteTimestamp(n)
			if !ts.IsZero() && ts.Before(*since) {
				skipped++
				continue
			}
		}
		if len(tagFilters) > 0 && !noteMatchesAnyTag(n, tagFilters) {
			skipped++
			continue
		}
		selected = append(selected, n)
	}

	if maxItems > 0 && len(selected) > maxItems {
		skipped += len(selected) - maxItems
		selected = selected[:maxItems]
	}

	if len(selected) == 0 {
		return fmt.Errorf("no evernote notes matched the selected scope")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Evernote notes selected: %d (dry-run)\n", len(selected))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(selected)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			n := selected[i]
			ts := noteTimestamp(n)
			tsLabel := "-"
			if !ts.IsZero() {
				tsLabel = ts.UTC().Format(time.RFC3339)
			}
			tagLabel := "-"
			if len(n.Tags) > 0 {
				tagLabel = strings.Join(n.Tags, ",")
			}
			fmt.Printf("%d. %s (id=%s, updated=%s, tags=%s)\n", i+1, n.Title, n.ExternalID, tsLabel, tagLabel)
		}
		if len(selected) > previewLimit {
			fmt.Printf("... and %d more notes\n", len(selected)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, n := range selected {
		content := evernoteimporter.RenderContent(n)
		source := strings.TrimSpace(n.SourceURL)
		if source == "" {
			source = "import:evernote"
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

	fmt.Printf("Evernote notes processed: %d\n", len(selected))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d evernote notes (first error: %w)", failed, firstErr)
	}

	return nil
}

func noteTimestamp(n evernoteimporter.Note) time.Time {
	if !n.Updated.IsZero() {
		return n.Updated
	}
	return n.Created
}

func normalizeTagFilters(tags []string) map[string]struct{} {
	out := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		out[tag] = struct{}{}
	}
	return out
}

func noteMatchesAnyTag(note evernoteimporter.Note, filters map[string]struct{}) bool {
	for _, tag := range note.Tags {
		if _, ok := filters[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}

func describeEvernoteScope(since *time.Time, tags map[string]struct{}, maxItems int) string {
	parts := make([]string, 0, 3)
	if since != nil {
		parts = append(parts, "since="+since.UTC().Format(time.RFC3339))
	}
	if len(tags) > 0 {
		flat := make([]string, 0, len(tags))
		for tag := range tags {
			flat = append(flat, tag)
		}
		sort.Strings(flat)
		parts = append(parts, "tags="+strings.Join(flat, ","))
	}
	if maxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", maxItems))
	}
	if len(parts) == 0 {
		return "all notes from export file"
	}
	return strings.Join(parts, "; ")
}
