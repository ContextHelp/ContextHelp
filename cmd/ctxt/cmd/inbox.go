package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/spf13/cobra"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Manage inbox items",
	Long: `List, triage, discard, and clear inbox items.

Inbox items are captured content awaiting a decision — triage to enqueue
for pipeline processing, or discard to remove from the inbox.

Examples:
  # List inbox items
  ctxt inbox list

  # Triage an item (promote to active and enqueue)
  ctxt inbox triage <id>

  # Triage with a specific pipeline
  ctxt inbox triage <id> --pipeline default

  # Discard an item
  ctxt inbox discard <id>

  # Clear all inbox items
  ctxt inbox clear`,
}

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List inbox items",
	RunE:  runInboxList,
}

var inboxTriageCmd = &cobra.Command{
	Use:   "triage <id>",
	Short: "Promote an inbox item to active and enqueue it",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxTriage,
}

var inboxDiscardCmd = &cobra.Command{
	Use:   "discard <id>",
	Short: "Discard an inbox item",
	Args:  cobra.ExactArgs(1),
	RunE:  runInboxDiscard,
}

var inboxClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Discard all inbox items",
	RunE:  runInboxClear,
}

func init() {
	rootCmd.AddCommand(inboxCmd)
	inboxCmd.AddCommand(inboxListCmd)
	inboxCmd.AddCommand(inboxTriageCmd)
	inboxCmd.AddCommand(inboxDiscardCmd)
	inboxCmd.AddCommand(inboxClearCmd)

	inboxListCmd.Flags().Int("limit", 50, "maximum results")
	inboxListCmd.Flags().Int("offset", 0, "pagination offset")
	inboxListCmd.Flags().String("before", "", "created before (RFC3339)")
	inboxListCmd.Flags().String("after", "", "created after (RFC3339)")

	inboxTriageCmd.Flags().String("pipeline", "", "pipeline to use (default: auto-detect)")
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")

	filter := service.InboxFilter{Limit: limit, Offset: offset}

	items, total, err := svc.ListInbox(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{"items": items, "total": total})
	}

	fmt.Printf("Inbox (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Source", "Note", "Created"}
	var rows [][]string
	for _, obj := range items {
		note := obj.InboxNote
		if len(note) > 30 {
			note = note[:27] + "..."
		}
		source := obj.Source
		if len(source) > 30 {
			source = source[:27] + "..."
		}
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			source,
			note,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runInboxTriage(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	id := args[0]
	pipeline, _ := cmd.Flags().GetString("pipeline")

	jobID, err := svc.TriageInbox(context.Background(), id, service.TriageRequest{Pipeline: pipeline})
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("inbox item %s not found", id)
		}
		return fmt.Errorf("triage: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]string{"job_id": jobID})
	}
	fmt.Printf("Triaged %s → job %s\n", id, jobID)
	return nil
}

func runInboxDiscard(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	id := args[0]
	if err := svc.DiscardInbox(context.Background(), id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("inbox item %s not found", id)
		}
		return fmt.Errorf("discard: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]string{"id": id, "status": "discarded"})
	}
	fmt.Printf("Discarded %s\n", id)
	return nil
}

func runInboxClear(_ *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	n, err := svc.ClearInbox(context.Background())
	if err != nil {
		return fmt.Errorf("clear inbox: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]int{"cleared": n})
	}
	fmt.Printf("Cleared %d inbox item(s)\n", n)
	return nil
}
