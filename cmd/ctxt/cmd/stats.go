package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show a live at-a-glance summary of the system",
	Long: `Print a summary of knowledge objects, jobs, entities, feeds, profiles,
reminders, and resurfacing candidates.

Examples:
  ctxt stats
  ctxt stats --output json
  ctxt stats --watch`,
	RunE: runStats,
}

func init() {
	rootCmd.AddCommand(statsCmd)
	statsCmd.Flags().Bool("watch", false, "re-print every 3s (Ctrl-C to exit)")
}

// statsData holds all counts for one snapshot.
type statsData struct {
	KnowledgeObjects int            `json:"knowledge_objects"`
	ObjectsByType    map[string]int `json:"objects_by_type"`
	Jobs             jobStats       `json:"jobs"`
	Entities         int            `json:"entities"`
	Feeds            int            `json:"feeds"`
	ActiveFeeds      int            `json:"feeds_active"`
	Profiles         int            `json:"profiles"`
	DefaultProfile   string         `json:"default_profile,omitempty"`
	Reminders        int            `json:"reminders_pending"`
	Resurfacing      int            `json:"resurfacing_candidates"`
}

type jobStats struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

func gatherStats(ctx context.Context) (*statsData, error) {
	svc, cleanup, err := newService()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	d := &statsData{
		ObjectsByType: make(map[string]int),
	}

	// Knowledge objects — count all, then break down by type.
	objs, total, err := svc.ListObjects(ctx, storage.ObjectFilter{Limit: 0, Status: "all"})
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	d.KnowledgeObjects = total
	for _, o := range objs {
		d.ObjectsByType[o.Type]++
	}

	// Jobs by status.
	for _, st := range []storage.JobStatus{
		storage.JobPending,
		storage.JobRunning,
		storage.JobCompleted,
		storage.JobFailed,
	} {
		_, n, err := svc.ListJobs(ctx, storage.JobFilter{Status: st, Limit: 0})
		if err != nil {
			return nil, fmt.Errorf("list jobs (%s): %w", st, err)
		}
		d.Jobs.Total += n
		switch st {
		case storage.JobPending:
			d.Jobs.Pending = n
		case storage.JobRunning:
			d.Jobs.Running = n
		case storage.JobCompleted:
			d.Jobs.Completed = n
		case storage.JobFailed:
			d.Jobs.Failed = n
		}
	}

	// Entities.
	entities, err := svc.ListEntities(ctx, storage.EntityFilter{})
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	d.Entities = len(entities)

	// Feeds.
	feeds, err := svc.ListFeeds(ctx, storage.FeedFilter{})
	if err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	d.Feeds = len(feeds)
	for _, f := range feeds {
		if f.Status == "active" {
			d.ActiveFeeds++
		}
	}

	// Profiles (from config — no DB call needed).
	if cfg != nil {
		d.Profiles = len(cfg.Profile.Profiles)
		d.DefaultProfile = cfg.Profile.Default
	}

	// Pending reminders.
	reminders, err := svc.ListPendingReminders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	d.Reminders = len(reminders)

	// Resurfacing candidates (use profile default, limit 1000 for count).
	resurfacing, err := svc.ListResurfacing(ctx, activeProfileName(), 1000, 0)
	if err != nil {
		return nil, fmt.Errorf("list resurfacing: %w", err)
	}
	d.Resurfacing = len(resurfacing)

	return d, nil
}

func printStats(d *statsData) {
	// Build type breakdown string e.g. "text: 1842 · url: 327"
	typeBreakdown := ""
	if len(d.ObjectsByType) > 0 {
		var parts []string
		// Stable sort: text first, url second, then remainder alphabetically.
		order := []string{"text", "url"}
		seen := map[string]bool{}
		for _, k := range order {
			if v, ok := d.ObjectsByType[k]; ok {
				parts = append(parts, fmt.Sprintf("%s: %d", k, v))
				seen[k] = true
			}
		}
		for k, v := range d.ObjectsByType {
			if !seen[k] {
				parts = append(parts, fmt.Sprintf("%s: %d", k, v))
			}
		}
		if len(parts) > 0 {
			typeBreakdown = " ("
			for i, p := range parts {
				if i > 0 {
					typeBreakdown += " · "
				}
				typeBreakdown += p
			}
			typeBreakdown += ")"
		}
	}

	defaultProfileStr := ""
	if d.DefaultProfile != "" {
		defaultProfileStr = fmt.Sprintf("  (default: %s)", d.DefaultProfile)
	}

	fmt.Fprintf(os.Stdout, "%-22s%5d%s\n", "Knowledge Objects", d.KnowledgeObjects, typeBreakdown)
	fmt.Fprintf(os.Stdout, "%-22s%5d  pending: %d · running: %d · completed: %d · failed: %d\n",
		"Jobs", d.Jobs.Total, d.Jobs.Pending, d.Jobs.Running, d.Jobs.Completed, d.Jobs.Failed)
	fmt.Fprintf(os.Stdout, "%-22s%5d\n", "Entities", d.Entities)
	fmt.Fprintf(os.Stdout, "%-22s%5d  (active: %d)\n", "Feeds", d.Feeds, d.ActiveFeeds)
	fmt.Fprintf(os.Stdout, "%-22s%5d%s\n", "Profiles", d.Profiles, defaultProfileStr)
	fmt.Fprintf(os.Stdout, "%-22s%5d  pending\n", "Reminders", d.Reminders)
	fmt.Fprintf(os.Stdout, "%-22s%5d  candidates\n", "Resurfacing", d.Resurfacing)
}

func runStats(cmd *cobra.Command, _ []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	ctx := context.Background()

	if !watch {
		d, err := gatherStats(ctx)
		if err != nil {
			return err
		}
		if isJSONOutput() {
			return outputJSON(os.Stdout, d)
		}
		printStats(d)
		return nil
	}

	// Watch mode: re-print every 3s until Ctrl-C.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Print immediately, then on each tick.
	printOnce := func() {
		d, err := gatherStats(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stats: %v\n", err)
			return
		}
		if isJSONOutput() {
			_ = outputJSON(os.Stdout, d)
		} else {
			// Clear screen for watch mode.
			fmt.Fprint(os.Stdout, "\033[H\033[2J")
			fmt.Fprintf(os.Stdout, "ctxt stats  (refreshing every 3s — Ctrl-C to quit)\n\n")
			printStats(d)
		}
	}

	printOnce()
	for {
		select {
		case <-ticker.C:
			printOnce()
		case <-sig:
			return nil
		}
	}
}
