package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	slackimporter "github.com/ideacrafterslabs/ctxt/internal/importer/slack"
	"github.com/spf13/cobra"
)

var importSlackCmd = &cobra.Command{
	Use:   "slack",
	Short: "Import messages from a Slack workspace export",
	Long: `Import messages from a local Slack workspace export directory.

The Slack workspace export format is a directory containing:
  - channels.json   (channel metadata)
  - <channel-name>/ (one subdirectory per channel)
    - YYYY-MM-DD.json (one file per day, containing message arrays)

Required from user:
  1) --dir path to unpacked Slack export directory

Selective import scope:
  --since RFC3339 or YYYY-MM-DD     only import messages on or after this time
  --channel <name> (repeatable)     include only messages from these channels
  --max-items N                     cap selected messages

Examples:
  # Dry-run and preview messages
  ctxt import slack --dir ./slack-export --dry-run

  # Import only recent messages from #general
  ctxt import slack --dir ./slack-export --since 2026-01-01 --channel general

  # Import all with a server override
  ctxt import slack --dir ./slack-export --server http://localhost:8080`,
	RunE: runImportSlack,
}

func init() {
	importCmd.AddCommand(importSlackCmd)

	importSlackCmd.Flags().String("dir", "", "path to unpacked Slack export directory")
	importSlackCmd.Flags().String("since", "", "only import messages on or after this time (RFC3339 or YYYY-MM-DD)")
	importSlackCmd.Flags().StringSlice("channel", nil, "only include messages from these channels (repeatable)")
	importSlackCmd.Flags().Int("max-items", 0, "maximum messages to import (0 = all)")
	importSlackCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importSlackCmd.Flags().String("pipeline", "", "pipeline override for enqueued jobs")

	importSlackCmd.MarkFlagRequired("dir")

	cliconv.WithSideEffect(importSlackCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(importSlackCmd, cliconv.IdempotencyConditional)
}

func runImportSlack(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	sinceRaw, _ := cmd.Flags().GetString("since")
	channels, _ := cmd.Flags().GetStringSlice("channel")
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

	msgs, err := slackimporter.ParseExportDir(dir)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return fmt.Errorf("no slack messages found in %s", dir)
	}

	// Build channel filter set.
	channelFilter := normalizeChannelFilters(channels)

	scanned := len(msgs)
	skipped := 0
	selected := make([]slackimporter.Message, 0, len(msgs))

	for _, m := range msgs {
		if since != nil && m.Timestamp.Before(*since) {
			skipped++
			continue
		}
		if len(channelFilter) > 0 {
			if _, ok := channelFilter[strings.ToLower(m.ChannelName)]; !ok {
				skipped++
				continue
			}
		}
		selected = append(selected, m)
	}

	if maxItems > 0 && len(selected) > maxItems {
		skipped += len(selected) - maxItems
		selected = selected[:maxItems]
	}

	if len(selected) == 0 {
		return fmt.Errorf("no slack messages matched the selected scope")
	}

	if dryRun {
		const previewLimit = 20
		fmt.Printf("Slack messages selected: %d (dry-run)\n", len(selected))
		fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)

		preview := len(selected)
		if preview > previewLimit {
			preview = previewLimit
		}
		for i := 0; i < preview; i++ {
			m := selected[i]
			sender := m.Username
			if sender == "" {
				sender = m.UserID
			}
			fmt.Printf("%d. [#%s] %s (%s): %s\n",
				i+1, m.ChannelName, sender,
				m.Timestamp.UTC().Format(time.RFC3339),
				truncateText(m.Text, 60))
		}
		if len(selected) > previewLimit {
			fmt.Printf("... and %d more messages\n", len(selected)-previewLimit)
		}
		return nil
	}

	var (
		success  int
		failed   int
		firstErr error
	)

	for _, m := range selected {
		content := slackimporter.RenderContent(m)
		source := m.Source
		if source == "" {
			source = "import:slack"
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

	fmt.Printf("Slack messages processed: %d\n", len(selected))
	fmt.Printf("Scanned: %d, Skipped: %d\n", scanned, skipped)
	fmt.Printf("Jobs enqueued: %d\n", success)
	if failed > 0 {
		fmt.Printf("Jobs failed: %d\n", failed)
	}

	if firstErr != nil {
		return fmt.Errorf("failed to enqueue %d slack messages (first error: %w)", failed, firstErr)
	}

	return nil
}

// normalizeChannelFilters converts a slice of channel names to a lowercase set.
func normalizeChannelFilters(channels []string) map[string]struct{} {
	out := make(map[string]struct{}, len(channels))
	for _, ch := range channels {
		ch = strings.ToLower(strings.TrimSpace(ch))
		if ch == "" {
			continue
		}
		out[ch] = struct{}{}
	}
	return out
}

// truncateText shortens text to maxLen characters adding "..." if truncated.
func truncateText(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	// Replace newlines for single-line preview.
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
