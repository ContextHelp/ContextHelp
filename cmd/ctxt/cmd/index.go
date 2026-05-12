package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [topic]",
	Short: "Show or refresh the topic index",
	Long: `Display the auto-generated topic index — a compressed summary layer
organized by category. Built from enrichment.structured_metadata topics.

Examples:
  # Show full topic index
  ctxt index

  # Show entries for a specific topic
  ctxt index auth

  # Regenerate the index from scratch
  ctxt index --refresh`,
	RunE: runIndex,
}

func init() {
	rootCmd.AddCommand(indexCmd)
	cliconv.WithSideEffect(indexCmd, cliconv.SideEffectWrite)
	// "index" is not in kit's defaultIdempotency table; --refresh rebuilds
	// the topic index in place, so rerunning is safe and converges to the
	// same projected state. Mark idempotent.
	cliconv.WithIdempotency(indexCmd, cliconv.IdempotencyYes)
	indexCmd.Flags().Bool("refresh", false,
		"regenerate the index from scratch")
}

func runIndex(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	refresh, _ := cmd.Flags().GetBool("refresh")

	if refresh {
		id, err := svc.BuildTopicIndex(ctx)
		if err != nil {
			return fmt.Errorf("rebuild index: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Index rebuilt (%s)\n", id)
	}

	// Single topic lookup.
	if len(args) > 0 {
		return printTopicEntries(svc, ctx, args[0])
	}

	return printFullIndex(svc, ctx)
}

func printTopicEntries(
	svc *service.Service, ctx context.Context, topic string,
) error {
	entries, err := svc.GetTopicEntries(ctx, topic)
	if err != nil {
		return err
	}
	if entries == nil {
		fmt.Println("No index found. Run: ctxt index --refresh")
		return nil
	}
	if len(entries) == 0 {
		fmt.Printf("No entries for topic %q\n", topic)
		return nil
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, entries)
	}

	fmt.Printf("Topic: %s (%d entries)\n\n", topic, len(entries))
	headers := []string{"Object ID", "Summary"}
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.ObjectID, truncate(e.Summary, 80)})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func printFullIndex(
	svc *service.Service, ctx context.Context,
) error {
	idx, err := svc.GetTopicIndex(ctx)
	if err != nil {
		return err
	}
	if idx == nil {
		fmt.Println("No index found. Run: ctxt index --refresh")
		return nil
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, idx)
	}

	fmt.Printf("Topic Index (%d categories, %d objects)\n\n",
		len(idx.Categories), len(idx.ObjectIDs))

	for _, cat := range idx.Categories {
		fmt.Printf("## %s (%d)\n", cat.Topic, len(cat.Entries))
		for _, e := range cat.Entries {
			fmt.Printf("  - [%s] %s\n", e.ObjectID, truncate(e.Summary, 70))
		}
		fmt.Println()
	}
	return nil
}
