package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
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
	Long: `List inbox items awaiting triage.

Filter by status (--pending, --failed, --raw) to switch into the job-queue
view; without filters the traditional inbox listing is shown.

Examples:
  ctxt inbox list
  ctxt inbox list --pending
  ctxt inbox list --limit 100 --offset 50`,
	RunE: runInboxList,
}

var inboxTriageCmd = &cobra.Command{
	Use:   "triage <id>",
	Short: "Promote an inbox item to active and enqueue it",
	Long: `Promote a single inbox item to active state and enqueue it for the
ingestion pipeline. Optionally pin a pipeline with --pipeline.

Examples:
  ctxt inbox triage abc123
  ctxt inbox triage abc123 --pipeline default`,
	Args: cobra.ExactArgs(1),
	RunE: runInboxTriage,
}

var inboxDiscardCmd = &cobra.Command{
	Use:   "discard <id>",
	Short: "Discard an inbox item",
	Long: `Permanently discard a single inbox item by ID. The item is removed
from the inbox queue and cannot be recovered.

Requires --confirm=yes (or --confirm=prompt for an interactive confirmation).

Examples:
  ctxt inbox discard abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runInboxDiscard,
}

var inboxClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Discard all inbox items",
	Long: `Discard every item currently sitting in the inbox. Destructive — the
discarded items cannot be recovered.

Requires --confirm=yes (or --confirm=prompt for an interactive confirmation).

Examples:
  ctxt inbox clear`,
	RunE: runInboxClear,
}

func init() {
	rootCmd.AddCommand(inboxCmd)
	inboxCmd.AddCommand(inboxListCmd)
	inboxCmd.AddCommand(inboxTriageCmd)
	inboxCmd.AddCommand(inboxDiscardCmd)
	inboxCmd.AddCommand(inboxClearCmd)

	cliconv.WithSideEffect(inboxListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(inboxTriageCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(inboxTriageCmd, cliconv.IdempotencyNo)
	cliconv.WithSideEffect(inboxDiscardCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(inboxDiscardCmd)
	cliconv.WithIdempotency(inboxDiscardCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(inboxClearCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(inboxClearCmd)
	cliconv.WithIdempotency(inboxClearCmd, cliconv.IdempotencyYes)

	inboxListCmd.Flags().Int("limit", 50, "maximum results")
	inboxListCmd.Flags().Int("offset", 0, "pagination offset")
	inboxListCmd.Flags().String("before", "", "created before (RFC3339)")
	inboxListCmd.Flags().String("after", "", "created after (RFC3339)")
	inboxListCmd.Flags().Bool("pending", false, "show only pending/running jobs")
	inboxListCmd.Flags().Bool("failed", false, "show only failed jobs")
	inboxListCmd.Flags().Bool("raw", false, "show only unenriched raw objects")

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
	pending, _ := cmd.Flags().GetBool("pending")
	failed, _ := cmd.Flags().GetBool("failed")
	raw, _ := cmd.Flags().GetBool("raw")

	// Queue view: --pending/--failed/--raw queries jobs+raw objects.
	if pending || failed || raw {
		qf := service.InboxQueueFilter{
			Pending: pending,
			Failed:  failed,
			Raw:     raw,
			Limit:   limit,
			Offset:  offset,
		}
		items, total, err := svc.ListInboxQueue(context.Background(), qf)
		if err != nil {
			return fmt.Errorf("list inbox queue: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(os.Stdout, map[string]any{"items": items, "total": total})
		}
		fmt.Printf("Inbox queue (%d total)\n\n", total)
		headers := []string{"ID", "Kind", "Status", "Type", "Source", "Pipeline", "Created"}
		var rows [][]string
		for _, item := range items {
			source := item.Source
			if len(source) > 35 {
				source = source[:32] + "..."
			}
			rows = append(rows, []string{
				item.ID,
				item.Kind,
				item.Status,
				item.Type,
				source,
				item.Pipeline,
				item.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		printTable(os.Stdout, headers, rows)
		return nil
	}

	// Traditional inbox view (no queue flags set).
	filter := service.InboxFilter{Limit: limit, Offset: offset}

	objs, total, err := svc.ListInbox(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{"items": objs, "total": total})
	}

	fmt.Printf("Inbox (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Source", "Note", "Created"}
	var rows [][]string
	for _, obj := range objs {
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
