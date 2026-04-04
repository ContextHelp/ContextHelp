package remind

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// ReminderBackend abstracts the storage calls needed by Checker.
type ReminderBackend interface {
	ListDueReminders(ctx context.Context, now time.Time) ([]*DueReminder, error)
	MarkReminded(ctx context.Context, id string, now time.Time) error
}

// DueReminder is a minimal view of a knowledge object with a due reminder.
type DueReminder struct {
	ID       string
	Title    string
	RemindAt time.Time
}

// Checker polls for due reminders and emits desktop notifications.
type Checker struct {
	backend  ReminderBackend
	interval time.Duration
}

// NewChecker creates a Checker with the given poll interval (default 1 minute).
func NewChecker(backend ReminderBackend, interval time.Duration) *Checker {
	if interval <= 0 {
		interval = time.Minute
	}
	return &Checker{backend: backend, interval: interval}
}

// Run starts the reminder check loop; blocks until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Check immediately on start.
	c.check(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.check(ctx)
		}
	}
}

func (c *Checker) check(ctx context.Context) {
	now := time.Now()
	dues, err := c.backend.ListDueReminders(ctx, now)
	if err != nil {
		// Non-fatal; log and continue.
		fmt.Printf("reminder checker: list due: %v\n", err)
		return
	}
	for _, r := range dues {
		notify(r.Title, fmt.Sprintf("Reminder set for %s", r.RemindAt.Format("15:04")))
		if err := c.backend.MarkReminded(ctx, r.ID, now); err != nil {
			fmt.Printf("reminder checker: mark reminded %s: %v\n", r.ID, err)
		}
	}
}

// notify sends a desktop notification using the appropriate system command.
func notify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(
			`display notification %q with title "ctxt reminder" subtitle %q`,
			body, title,
		)
		_ = exec.Command("osascript", "-e", script).Run() // #nosec G204 -- script is constructed from reminder title/body
	case "linux":
		_ = exec.Command("notify-send", // #nosec G204 -- args are reminder title/body
			"--app-name=ctxt",
			fmt.Sprintf("ctxt: %s", title),
			body,
		).Run()
	default:
		// Fallback: log to stdout.
		fmt.Printf("[reminder] %s — %s\n", title, body)
	}
}

// ─── Service adapter ─────────────────────────────────────────────────────────

// ServiceBackend adapts service.Service to ReminderBackend without importing
// the service package (avoids circular imports).
type ServiceBackend struct {
	ListDue  func(ctx context.Context, now time.Time) ([]*DueReminder, error)
	MarkDone func(ctx context.Context, id string, now time.Time) error
}

func (b *ServiceBackend) ListDueReminders(ctx context.Context, now time.Time) ([]*DueReminder, error) {
	return b.ListDue(ctx, now)
}

func (b *ServiceBackend) MarkReminded(ctx context.Context, id string, now time.Time) error {
	return b.MarkDone(ctx, id, now)
}
