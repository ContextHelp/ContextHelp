package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	obsidianImporter "github.com/ideacrafterslabs/ctxt/internal/importer/obsidian"
	"github.com/spf13/cobra"
)

var importObsidianCmd = &cobra.Command{
	Use:   "obsidian",
	Short: "Import an Obsidian vault",
	Long: `Import notes from a local Obsidian vault directory.

Required from user:
  1) --vault path to your Obsidian vault directory

Selective import scope:
  --since RFC3339 or YYYY-MM-DD     only import notes modified on or after this time
  --max-items N                     cap selected notes
  --dry-run                         preview selected notes without importing

Examples:
  # Dry-run and inspect selected notes
  ctxt import obsidian --vault ~/Documents/MyVault --dry-run

  # Import notes modified since a date
  ctxt import obsidian --vault ~/Documents/MyVault --since 2026-01-01

  # Import with a custom server
  ctxt import obsidian --vault ~/Documents/MyVault --server http://localhost:8080`,
	RunE: runImportObsidian,
}

func init() {
	importCmd.AddCommand(importObsidianCmd)

	importObsidianCmd.Flags().String("vault", "", "path to Obsidian vault directory")
	importObsidianCmd.Flags().String("since", "", "only import notes modified since this time (RFC3339 or YYYY-MM-DD)")
	importObsidianCmd.Flags().Int("max-items", 0, "maximum notes to import (0 = all)")
	importObsidianCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importObsidianCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	importObsidianCmd.MarkFlagRequired("vault")

	cliconv.WithSideEffect(importObsidianCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importObsidianCmd, cliconv.IdempotencyConditional)

	cliconv.WithExamples(importObsidianCmd, []cliconv.Example{
		{
			Title:   "Import from default source",
			Command: "ctxt import obsidian --vault ~/Obsidian",
		},
		{
			Title:   "Dry-run preview",
			Command: "ctxt import obsidian --vault ~/Obsidian --confirm=no --dry-run",
		},
	})
	cliconv.WithNextSteps(importObsidianCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "verify imported items"},
		{When: "on success", Suggest: "ctxt find <keyword>", Reason: "search the newly-imported data"},
	})
}

func runImportObsidian(cmd *cobra.Command, args []string) error {
	vault, _ := cmd.Flags().GetString("vault")
	sinceRaw, _ := cmd.Flags().GetString("since")
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

	notes, err := obsidianImporter.WalkVault(vault)
	if err != nil {
		return err
	}
	if len(notes) == 0 {
		return fmt.Errorf("no markdown notes found in vault %s", vault)
	}

	fmt.Printf("Obsidian scope: %s\n", describeObsidianScope(since, maxItems))

	scanned := len(notes)
	skipped := 0
	selected := make([]obsidianImporter.Note, 0, len(notes))

	for _, n := range notes {
		if since != nil && !n.ModTime.IsZero() && n.ModTime.Before(*since) {
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
		return fmt.Errorf("no Obsidian notes matched the selected scope")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Obsidian notes selected: %d (dry-run)\n", len(selected))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(selected)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			n := selected[i]
			modLabel := "-"
			if !n.ModTime.IsZero() {
				modLabel = n.ModTime.UTC().Format(time.RFC3339)
			}
			tagLabel := "-"
			if len(n.Tags) > 0 {
				tagLabel = strings.Join(n.Tags, ",")
			}
			fmt.Printf("%d. %s (id=%s, path=%s, modified=%s, tags=%s)\n",
				i+1, n.Title, n.ExternalID, n.RelPath, modLabel, tagLabel)
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
		content := obsidianImporter.RenderContent(n)
		source := "import:obsidian:" + n.RelPath

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

	fmt.Printf("Obsidian notes processed: %d\n", len(selected))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d Obsidian notes (first error: %w)", failed, firstErr)
	}

	return nil
}

func describeObsidianScope(since *time.Time, maxItems int) string {
	parts := make([]string, 0, 2)
	if since != nil {
		parts = append(parts, "since="+since.UTC().Format(time.RFC3339))
	}
	if maxItems > 0 {
		parts = append(parts, fmt.Sprintf("max-items=%d", maxItems))
	}
	if len(parts) == 0 {
		return "all notes from vault"
	}
	return strings.Join(parts, "; ")
}
