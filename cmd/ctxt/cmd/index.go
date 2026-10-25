package cmd

import (
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

var indexCmd = &cobra.Command{
	Use:   "index [topic]",
	Short: "Show or refresh the topic index",
	Long: `Display the auto-generated topic index — a compressed summary layer
organized by category. Built from enrichment.structured_metadata topics.

Calls the dpkms API of the resolved instance (--server, --instance,
CTXT_INSTANCE, the current instance, then config). Reading needs the
read:objects scope; --refresh rebuilds the index on dpkms and needs
write:objects. Before the first rebuild there is no index (exit 3).

Examples:
  # Show full topic index
  ctxt index

  # Show entries for a specific topic
  ctxt index auth

  # Regenerate the index from scratch
  ctxt index --refresh`,
	Args: cobra.MaximumNArgs(1),
	RunE: runIndex,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	cliconv.WithSideEffect(indexCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(indexCmd, []cliconv.Example{
		{Title: "Show the full topic index", Command: "ctxt index"},
		{Title: "Show entries for one topic", Command: "ctxt index auth"},
		{Title: "Regenerate the index", Command: "ctxt index --refresh"},
	})
	cliconv.WithNextSteps(indexCmd, []cliconv.NextStep{
		{When: "after --refresh", Suggest: "ctxt index", Reason: "view the rebuilt topic categories"},
	})
	// "index" is not in kit's defaultIdempotency table; --refresh rebuilds
	// the topic index in place, so rerunning is safe and converges to the
	// same projected state. Mark idempotent.
	cliconv.WithIdempotency(indexCmd, cliconv.IdempotencyYes)
	indexCmd.Flags().Bool("refresh", false,
		"regenerate the index from scratch")
}

// topicIndexError gives a dpkms 404 (no index built yet) the recovery a
// caller needs; every other failure keeps the class the client gave it.
func topicIndexError(err error) error {
	var ke *output.Error
	if !errors.As(err, &ke) || ke.ExitCode != output.ExitNotFound {
		return fmt.Errorf("topic index: %w", err)
	}
	e := output.NotFoundError("no topic index: it has not been built yet")
	e.SuggestedFix = "run `ctxt index --refresh` to build it (needs write:objects)"
	return e
}

func runIndex(cmd *cobra.Command, args []string) error {
	refresh, _ := cmd.Flags().GetBool("refresh")
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}

	if refresh {
		id, err := client.RebuildTopicIndex(cmd.Context())
		if err != nil {
			return fmt.Errorf("rebuild topic index: %w", err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Index rebuilt (%s)\n", id)
	}

	if len(args) > 0 {
		return printTopicEntries(cmd, client, args[0])
	}
	return printFullIndex(cmd, client)
}

func printTopicEntries(cmd *cobra.Command, client *dpkmsclient.Client, topic string) error {
	entries, err := client.TopicEntries(cmd.Context(), topic)
	if err != nil {
		return topicIndexError(err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, entries)
	}
	if len(entries) == 0 {
		fmt.Fprintf(out, "No entries for topic %q\n", topic)
		return nil
	}

	fmt.Fprintf(out, "Topic: %s (%d entries)\n\n", topic, len(entries))
	headers := []string{"Object ID", "Summary"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{e.ObjectID, truncate(e.Summary, 80)})
	}
	printTable(out, headers, rows)
	return nil
}

func printFullIndex(cmd *cobra.Command, client *dpkmsclient.Client) error {
	idx, err := client.TopicIndex(cmd.Context())
	if err != nil {
		return topicIndexError(err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, idx)
	}

	fmt.Fprintf(out, "Topic Index (%d categories, %d objects)\n\n",
		len(idx.Categories), len(idx.ObjectIDs))
	for _, cat := range idx.Categories {
		fmt.Fprintf(out, "## %s (%d)\n", cat.Topic, len(cat.Entries))
		for _, e := range cat.Entries {
			fmt.Fprintf(out, "  - [%s] %s\n", e.ObjectID, truncate(e.Summary, 70))
		}
		fmt.Fprintln(out)
	}
	return nil
}
