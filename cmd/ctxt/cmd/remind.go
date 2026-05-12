package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/remind"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

// ─── ctxt remind {set,clear,list} ────────────────────────────────────────────

var remindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Manage reminders on knowledge objects",
}

var remindSetCmd = &cobra.Command{
	Use:   "set <id> <time-expression>",
	Short: "Set a reminder on a knowledge object",
	Long: `Set a reminder on a knowledge object. The reminder will trigger a desktop
notification when dpkms serve is running.

Time expressions (case-insensitive):
  in 2h          in 30m            in 1h30m
  today 9am      tomorrow 14:30    monday 9am
  2026-04-01     2026-04-01 10:00

Examples:
  ctxt remind set abc123 "tomorrow 9am"
  ctxt remind set abc123 "in 2h"
  ctxt remind set abc123 "2026-04-01 10:00"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runRemindSet,
}

var remindClearCmd = &cobra.Command{
	Use:   "clear <id>",
	Short: "Clear a reminder from a knowledge object",
	Long: `Clear the reminder previously attached to a knowledge object. The
object itself is not modified — only its reminder schedule is removed.

Examples:
  ctxt remind clear abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runRemindClear,
}

var remindListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled reminders",
	Long: `List all knowledge objects with scheduled reminders.

Examples:
  ctxt remind list
  ctxt remind list --format json`,
	RunE: runRemindList,
}

func init() {
	rootCmd.AddCommand(remindCmd)
	remindCmd.AddCommand(remindSetCmd)
	remindCmd.AddCommand(remindClearCmd)
	remindCmd.AddCommand(remindListCmd)

	cliconv.WithSideEffect(remindSetCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(remindSetCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(remindSetCmd, []cliconv.Example{
		{Title: "Remind me tomorrow morning", Command: "ctxt remind set abc123 \"tomorrow 9am\""},
		{Title: "Remind in two hours", Command: "ctxt remind set abc123 \"in 2h\""},
	})
	cliconv.WithNextSteps(remindSetCmd, []cliconv.NextStep{
		{When: "after set", Suggest: "ctxt remind list", Reason: "confirm the reminder shows up with the expected fire time"},
	})
	cliconv.WithSideEffect(remindClearCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(remindClearCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(remindClearCmd, []cliconv.Example{
		{Title: "Clear a reminder", Command: "ctxt remind clear abc123"},
	})
	cliconv.WithNextSteps(remindClearCmd, []cliconv.NextStep{
		{When: "after clear", Suggest: "ctxt remind list", Reason: "verify the reminder is gone"},
	})
	cliconv.WithSideEffect(remindListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(remindListCmd, []cliconv.Example{
		{Title: "List scheduled reminders", Command: "ctxt remind list"},
		{Title: "Emit JSON", Command: "ctxt remind list --output json"},
	})
}

func runRemindSet(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	id := args[0]
	expr := strings.Join(args[1:], " ")
	at, err := remind.ParseTime(expr, time.Now())
	if err != nil {
		return err
	}

	if err := svc.SetObjectReminder(ctx, id, at); err != nil {
		return fmt.Errorf("set reminder: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Reminder set for %s at %s\n",
		id, at.Format("2006-01-02 15:04:05 MST"))
	return nil
}

func runRemindClear(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	id := args[0]
	if err := svc.ClearObjectReminder(ctx, id); err != nil {
		return fmt.Errorf("clear reminder: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Reminder cleared for %s\n", id)
	return nil
}

func runRemindList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	objects, err := svc.ListPendingReminders(ctx)
	if err != nil {
		return fmt.Errorf("list reminders: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"reminders": objects,
			"total":     len(objects),
		})
	}

	if len(objects) == 0 {
		fmt.Println("No reminders scheduled.")
		return nil
	}

	now := time.Now()
	fmt.Printf("Reminders (%d)\n\n", len(objects))
	headers := []string{"ID", "Title", "Remind At", "Status"}
	var rows [][]string
	for _, obj := range objects {
		title := objectTitle(obj)
		remindAt := obj.RemindAt.Format("2006-01-02 15:04 MST")
		status := reminderStatus(obj, now)
		rows = append(rows, []string{obj.ID, title, remindAt, status})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

// objectTitle returns a short display title for a knowledge object.
func objectTitle(obj *storage.KnowledgeObject) string {
	if len(obj.Summaries) > 0 {
		s := obj.Summaries[0]
		if len(s) > 40 {
			return s[:37] + "..."
		}
		return s
	}
	if len(obj.ID) > 16 {
		return obj.ID[:16] + "..."
	}
	return obj.ID
}

func reminderStatus(obj *storage.KnowledgeObject, now time.Time) string {
	if obj.RemindedAt != nil {
		return "notified"
	}
	if obj.RemindAt.Before(now) {
		return "overdue"
	}
	return "pending"
}
