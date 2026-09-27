package cmd

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Manage inbox items",
	Long: `List, triage, discard, and clear inbox items.

Inbox items are captured content awaiting a decision — triage to enqueue
for pipeline processing, or discard to remove from the inbox.

Every subcommand calls the dpkms API of the resolved instance (--server,
--instance, CTXT_INSTANCE, the current instance, then config). Listing
needs the read:inbox scope; triage, discard and clear need process:inbox,
which only the admin role holds.

Examples:
  # List inbox items
  ctxt inbox list

  # Triage an item (promote to active and enqueue)
  ctxt inbox triage <id>

  # Triage with a specific pipeline
  ctxt inbox triage <id> --pipeline default

  # Discard an item (the first run prints the confirm token)
  ctxt inbox discard <id> --confirm-token=<token>

  # Clear all inbox items
  ctxt inbox clear --confirm-token=<token>`,
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
	Long: `Discard a single inbox item by ID: it leaves the inbox and is never
processed.

Requires --confirm-token: run it once without and the refusal prints the
token.

Examples:
  ctxt inbox discard abc123 --confirm-token=<token>`,
	Args: cobra.ExactArgs(1),
	RunE: runInboxDiscard,
}

var inboxClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Discard all inbox items",
	Long: `Discard every item currently sitting in the inbox. Destructive — the
discarded items are never processed.

Requires --confirm-token: run it once without and the refusal prints the
token.

Examples:
  ctxt inbox clear --confirm-token=<token>`,
	RunE: runInboxClear,
}

func init() {
	rootCmd.AddCommand(inboxCmd)
	inboxCmd.AddCommand(inboxListCmd)
	inboxCmd.AddCommand(inboxTriageCmd)
	inboxCmd.AddCommand(inboxDiscardCmd)
	inboxCmd.AddCommand(inboxClearCmd)

	cliconv.WithSideEffect(inboxListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(inboxListCmd, []cliconv.Example{
		{Title: "List inbox items", Command: "ctxt inbox list"},
		{Title: "Show only pending jobs", Command: "ctxt inbox list --pending --limit 100"},
	})
	cliconv.WithSideEffect(inboxTriageCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(inboxTriageCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(inboxTriageCmd, []cliconv.Example{
		{Title: "Triage an inbox item", Command: "ctxt inbox triage abc123"},
		{Title: "Triage with a specific pipeline", Command: "ctxt inbox triage abc123 --pipeline default"},
	})
	cliconv.WithNextSteps(inboxTriageCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt job status <job-id>", Reason: "follow the triaged job through the pipeline"},
		{When: "if triage fails", Suggest: "ctxt inbox list --failed", Reason: "inspect failed jobs and decide on retry"},
	})
	cliconv.WithSideEffect(inboxDiscardCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(inboxDiscardCmd)
	cliconv.WithIdempotency(inboxDiscardCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(inboxDiscardCmd, []cliconv.Example{
		{Title: "Discard an inbox item", Command: "ctxt inbox discard abc123 --confirm-token=<token>"},
	})
	cliconv.WithNextSteps(inboxDiscardCmd, []cliconv.NextStep{
		{When: "after discard", Suggest: "ctxt inbox list", Reason: "verify the remaining inbox state"},
	})
	cliconv.WithSideEffect(inboxClearCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(inboxClearCmd)
	cliconv.WithIdempotency(inboxClearCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(inboxClearCmd, []cliconv.Example{
		{Title: "Clear every inbox item", Command: "ctxt inbox clear --confirm-token=<token>"},
	})
	cliconv.WithNextSteps(inboxClearCmd, []cliconv.NextStep{
		{When: "after clear", Suggest: "ctxt inbox list", Reason: "confirm the inbox is empty"},
	})

	inboxListCmd.Flags().Int("limit", 50, "maximum results")
	inboxListCmd.Flags().Int("offset", 0, "pagination offset")
	inboxListCmd.Flags().String("before", "", "created before (RFC3339)")
	inboxListCmd.Flags().String("after", "", "created after (RFC3339)")
	inboxListCmd.Flags().Bool("pending", false, "show only pending/running jobs")
	inboxListCmd.Flags().Bool("failed", false, "show only failed jobs")
	inboxListCmd.Flags().Bool("raw", false, "show only unenriched raw objects")

	inboxTriageCmd.Flags().String("pipeline", "", "pipeline to use (default: auto-detect)")

	for _, c := range []*cobra.Command{inboxListCmd, inboxTriageCmd, inboxDiscardCmd, inboxClearCmd} {
		c.Flags().String("server", "", serverFlagUsage)
	}
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	pending, _ := cmd.Flags().GetBool("pending")
	failed, _ := cmd.Flags().GetBool("failed")
	raw, _ := cmd.Flags().GetBool("raw")
	before, err := inboxTimeFlag(cmd, "before")
	if err != nil {
		return err
	}
	after, err := inboxTimeFlag(cmd, "after")
	if err != nil {
		return err
	}

	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	// Queue view: --pending/--failed/--raw lists jobs and raw objects.
	if pending || failed || raw {
		items, total, err := client.ListInboxQueue(cmd.Context(), dpkmsclient.InboxQueueRequest{
			Pending: pending, Failed: failed, Raw: raw, Limit: limit, Offset: offset,
		})
		if err != nil {
			return fmt.Errorf("list inbox queue: %w", err)
		}
		if isJSONOutput() {
			return outputJSON(out, map[string]any{"items": items, "total": total})
		}
		fmt.Fprintf(out, "Inbox queue (%d total)\n\n", total)
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
		printTable(out, headers, rows)
		return nil
	}

	objs, total, err := client.ListInbox(cmd.Context(), dpkmsclient.InboxListRequest{
		Limit: limit, Offset: offset, Before: before, After: after,
	})
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(out, map[string]any{"items": objs, "total": total})
	}

	fmt.Fprintf(out, "Inbox (%d total)\n\n", total)
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
	printTable(out, headers, rows)
	return nil
}

// inboxTimeFlag parses an RFC 3339 --before/--after value; unset is the
// zero time. A malformed value is a usage error.
func inboxTimeFlag(cmd *cobra.Command, name string) (time.Time, error) {
	v, _ := cmd.Flags().GetString(name)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		e := output.UsageError(fmt.Sprintf("--%s %q: want an RFC 3339 timestamp", name, v))
		e.SuggestedFix = "e.g. --" + name + " 2026-04-15T00:00:00Z"
		return time.Time{}, e
	}
	return t, nil
}

func runInboxTriage(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	id := args[0]
	pipeline, _ := cmd.Flags().GetString("pipeline")

	jobID, err := client.TriageInbox(cmd.Context(), id, pipeline)
	if err != nil {
		return inboxItemError("triage", id, err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]string{"job_id": jobID})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Triaged %s → job %s\n", id, jobID)
	return nil
}

func runInboxDiscard(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	id := args[0]
	if err := client.DiscardInbox(cmd.Context(), id); err != nil {
		return inboxItemError("discard", id, err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]string{"id": id, "status": "discarded"})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Discarded %s\n", id)
	return nil
}

// inboxItemError classifies a failed triage or discard of id. dpkms
// answers 404 for anything that is not an inbox item right now (never
// captured, already triaged, discarded): NOT_FOUND, exit 3, pointing at
// the list of items that can be processed. Every other failure keeps the
// class the dpkms client gave it.
func inboxItemError(verb, id string, err error) error {
	var re *dpkmsclient.RemoteError
	if !errors.As(err, &re) || re.StatusCode != http.StatusNotFound {
		return fmt.Errorf("%s inbox item %s: %w", verb, id, err)
	}
	e := output.NotFoundError(fmt.Sprintf("%s: no inbox item %s; only items still in the inbox can be triaged or discarded", verb, id))
	e.SuggestedFix = "run `ctxt inbox list` to see the items awaiting triage"
	return e
}

func runInboxClear(cmd *cobra.Command, _ []string) error {
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	n, err := client.ClearInbox(cmd.Context())
	if err != nil {
		return fmt.Errorf("clear inbox: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]int{"cleared": n})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Cleared %d inbox item(s)\n", n)
	return nil
}
