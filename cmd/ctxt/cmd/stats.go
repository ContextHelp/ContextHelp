package cmd

import (
	"context"
	"fmt"
	"io"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show a live at-a-glance summary of the system",
	Long: `Print a summary of knowledge objects, jobs, entities, feeds, profiles,
reminders, and resurfacing candidates.

Counts come from the dpkms API of the resolved instance (--server,
--instance, CTXT_INSTANCE, the current instance, then config) and need
the read:objects scope. Profiles are this client's configuration; the
resurfacing count is the default profile's.

Examples:
  ctxt stats
  ctxt stats --format json
  ctxt stats --watch`,
	RunE: runStats,
}

// statsInterval is the --watch refresh period.
const statsInterval = 3 * time.Second

func init() {
	rootCmd.AddCommand(statsCmd)
	cliconv.WithSideEffect(statsCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(statsCmd, []cliconv.Example{
		{Title: "Show a snapshot summary", Command: "ctxt stats"},
		{Title: "Re-print every 3 seconds", Command: "ctxt stats --watch"},
		{Title: "JSON output for automation", Command: "ctxt stats --format json"},
	})
	// "stats" is not in kit's defaultIdempotency table; aggregating counts
	// is naturally idempotent — no state changes between calls.
	cliconv.WithIdempotency(statsCmd, cliconv.IdempotencyYes)
	statsCmd.Flags().Bool("watch", false, "re-print every 3s (Ctrl-C to exit)")
	statsCmd.Flags().String("server", "", serverFlagUsage)
}

// statsData is one snapshot: the instance's counts plus this client's
// profiles.
type statsData struct {
	*dpkmsclient.Stats
	Profiles       int    `json:"profiles"`
	DefaultProfile string `json:"default_profile,omitempty"`
}

// gatherStats fetches the instance's counts, scoping the resurfacing
// count to the active profile, and adds the configured profiles.
func gatherStats(ctx context.Context, client *dpkmsclient.Client, profile string) (*statsData, error) {
	st, err := client.Stats(ctx, profile)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	d := &statsData{Stats: st}
	if cfg != nil {
		d.Profiles = len(cfg.Profile.Profiles)
		d.DefaultProfile = cfg.Profile.Default
	}
	return d, nil
}

// typeBreakdown renders counts as " (text: 3 · url: 2 · note: 1)": text
// and url first, then the other types by name.
func typeBreakdown(byType map[string]int) string {
	if len(byType) == 0 {
		return ""
	}
	keys := make([]string, 0, len(byType))
	for k := range byType {
		keys = append(keys, k)
	}
	rank := func(k string) int {
		switch k {
		case objectTypeText:
			return 0
		case objectTypeURL:
			return 1
		}
		return 2
	}
	sort.Slice(keys, func(i, j int) bool {
		if ri, rj := rank(keys[i]), rank(keys[j]); ri != rj {
			return ri < rj
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %d", k, byType[k]))
	}
	return " (" + strings.Join(parts, " · ") + ")"
}

func printStats(w io.Writer, d *statsData) {
	defaultProfile := ""
	if d.DefaultProfile != "" {
		defaultProfile = fmt.Sprintf("  (default: %s)", d.DefaultProfile)
	}

	fmt.Fprintf(w, "%-22s%5d%s\n", "Knowledge Objects", d.KnowledgeObjects, typeBreakdown(d.ObjectsByType))
	fmt.Fprintf(w, "%-22s%5d  pending: %d · running: %d · completed: %d · failed: %d\n",
		"Jobs", d.Jobs.Total, d.Jobs.Pending, d.Jobs.Running, d.Jobs.Completed, d.Jobs.Failed)
	fmt.Fprintf(w, "%-22s%5d\n", "Entities", d.Entities)
	fmt.Fprintf(w, "%-22s%5d  (active: %d)\n", "Feeds", d.Feeds, d.ActiveFeeds)
	fmt.Fprintf(w, "%-22s%5d%s\n", "Profiles", d.Profiles, defaultProfile)
	fmt.Fprintf(w, "%-22s%5d  pending\n", "Reminders", d.Reminders)
	if d.Resurfacing == nil {
		fmt.Fprintf(w, "%-22s%5s  candidates (not tracked by this instance's storage)\n", "Resurfacing", "-")
	} else {
		fmt.Fprintf(w, "%-22s%5d  candidates\n", "Resurfacing", *d.Resurfacing)
	}
}

func runStats(cmd *cobra.Command, _ []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	profile := activeProfileName(cmd)
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	render := func(d *statsData) error {
		if isJSONOutput() {
			return outputJSON(out, d)
		}
		if watch {
			// Clear the screen between snapshots.
			fmt.Fprint(out, "\033[H\033[2J")
			fmt.Fprintf(out, "ctxt stats  (refreshing every %s — Ctrl-C to quit)\n\n", statsInterval)
		}
		printStats(out, d)
		return nil
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The first snapshot decides the exit: a refused token or an instance
	// that does not answer fails the command, with or without --watch.
	d, err := gatherStats(ctx, client, profile)
	if err != nil {
		return err
	}
	if err := render(d); err != nil || !watch {
		return err
	}

	// Later failures are reported and the next tick retries.
	ticker := time.NewTicker(statsInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d, err := gatherStats(ctx, client, profile)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%v\n", err)
				continue
			}
			if err := render(d); err != nil {
				return err
			}
		}
	}
}
