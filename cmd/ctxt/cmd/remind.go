package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/remind"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

// ─── ctxt remind ─────────────────────────────────────────────────────────────

var remindCmd = &cobra.Command{
	Use:   "remind <id> <time-expression>",
	Short: "Set or clear a reminder on a knowledge object",
	Long: `Set a reminder on a knowledge object. The reminder will trigger a desktop
notification when dpkms serve is running.

Time expressions (case-insensitive):
  in 2h          in 30m            in 1h30m
  today 9am      tomorrow 14:30    monday 9am
  2026-04-01     2026-04-01 10:00

Examples:
  ctxt remind abc123 "tomorrow 9am"
  ctxt remind abc123 "in 2h"
  ctxt remind abc123 "2026-04-01 10:00"
  ctxt remind --clear abc123`,
	Args: func(cmd *cobra.Command, args []string) error {
		clear, _ := cmd.Flags().GetBool("clear")
		if clear {
			if len(args) != 1 {
				return fmt.Errorf("--clear requires exactly one argument: <id>")
			}
			return nil
		}
		if len(args) < 2 {
			return fmt.Errorf("requires <id> and <time-expression>")
		}
		return nil
	},
	RunE: runRemind,
}

func init() {
	rootCmd.AddCommand(remindCmd)
	remindCmd.Flags().Bool("clear", false, "remove reminder from the object")
}

func runRemind(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	id := args[0]

	clear, _ := cmd.Flags().GetBool("clear")
	if clear {
		if err := svc.ClearObjectReminder(ctx, id); err != nil {
			return fmt.Errorf("clear reminder: %w", err)
		}
		fmt.Fprintf(os.Stdout, "Reminder cleared for %s\n", id)
		return nil
	}

	// Join remaining args as the time expression (e.g. "tomorrow 9am" passed as two args).
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

// ─── ctxt reminders ──────────────────────────────────────────────────────────

var remindersCmd = &cobra.Command{
	Use:   "reminders",
	Short: "List scheduled reminders",
	Long: `List all knowledge objects with scheduled reminders.

Examples:
  ctxt reminders
  ctxt reminders --output json`,
	RunE: runReminders,
}

func init() {
	rootCmd.AddCommand(remindersCmd)
}

func runReminders(cmd *cobra.Command, args []string) error {
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
