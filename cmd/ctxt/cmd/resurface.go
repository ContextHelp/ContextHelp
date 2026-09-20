package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/resurfacing"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var resurfaceCmd = &cobra.Command{
	Use:   "resurface",
	Short: "Show top resurfacing candidates for the active profile",
	Long: `Print knowledge objects most relevant to the active profile right now.

Candidates are scored by entity overlap, tag overlap, and recency decay.
Run ctxt resurface refresh to re-score before showing results.

Examples:
  ctxt resurface
  ctxt resurface --limit 5
  ctxt resurface refresh
  ctxt resurface dismiss <entry-id>
  ctxt resurface --format json`,
	RunE: runResurfaceShow,
}

var resurfaceRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Re-score all objects and update the resurfacing queue",
	Long: `Re-score every knowledge object against the active profile and
rewrite the resurfacing queue. Safe to re-run; existing entries are
replaced with the freshly computed scores.

Examples:
  ctxt resurface refresh`,
	RunE: runResurfaceRefresh,
}

var resurfaceDismissCmd = &cobra.Command{
	Use:   "dismiss <entry-id>",
	Short: "Dismiss a resurfacing entry",
	Long: `Dismiss a single resurfacing entry by ID so it stops appearing in
'ctxt resurface' output. The underlying knowledge object is untouched.

Examples:
  ctxt resurface dismiss entry-abc123`,
	Args: cobra.ExactArgs(1),
	RunE: runResurfaceDismiss,
}

func init() {
	rootCmd.AddCommand(resurfaceCmd)
	resurfaceCmd.AddCommand(resurfaceRefreshCmd)
	resurfaceCmd.AddCommand(resurfaceDismissCmd)

	// resurface itself re-injects items into the queue when shown
	// (MarkResurfaced); treat as Write. refresh rewrites the queue.
	cliconv.WithSideEffect(resurfaceCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(resurfaceCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(resurfaceCmd, []cliconv.Example{
		{Title: "Show top resurfacing candidates", Command: "ctxt resurface"},
		{Title: "Cap the result count", Command: "ctxt resurface --limit 5"},
	})
	cliconv.WithNextSteps(resurfaceCmd, []cliconv.NextStep{
		{When: "when no results", Suggest: "ctxt resurface refresh", Reason: "re-score the queue before showing results"},
		{When: "to drop an entry", Suggest: "ctxt resurface dismiss <entry-id>", Reason: "remove uninteresting entries from future output"},
	})
	cliconv.WithSideEffect(resurfaceRefreshCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(resurfaceRefreshCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(resurfaceRefreshCmd, []cliconv.Example{
		{Title: "Re-score the resurfacing queue", Command: "ctxt resurface refresh"},
	})
	cliconv.WithNextSteps(resurfaceRefreshCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt resurface", Reason: "show the freshly re-scored candidates"},
	})
	cliconv.WithSideEffect(resurfaceDismissCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(resurfaceDismissCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(resurfaceDismissCmd, []cliconv.Example{
		{Title: "Dismiss a resurfacing entry", Command: "ctxt resurface dismiss entry-abc123"},
	})
	cliconv.WithNextSteps(resurfaceDismissCmd, []cliconv.NextStep{
		{When: "after dismiss", Suggest: "ctxt resurface", Reason: "verify the entry no longer appears"},
	})

	resurfaceCmd.Flags().IntP("limit", "n", 0, "max items to show (0 = use config default)")
	resurfaceCmd.Flags().Float64("min-score", 0, "minimum score threshold (0 = use config default)")
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func activeProfileName() string {
	if cfg != nil && cfg.Profile.Default != "" {
		return cfg.Profile.Default
	}
	return viper.GetString("profile.default")
}

func activeProfile() (string, config.FocusProfile, bool) {
	name := activeProfileName()
	if name == "" || cfg == nil {
		return "", config.FocusProfile{}, false
	}
	p, ok := cfg.Profile.Profiles[name]
	return name, p, ok
}

func resurfacingCfg() config.ResurfacingConfig {
	if cfg != nil {
		return cfg.Resurfacing
	}
	return config.ResurfacingConfig{
		Enabled:     true,
		MaxItems:    10,
		MinScore:    0.4,
		RunInterval: time.Hour,
	}
}

// ─── commands ────────────────────────────────────────────────────────────────

func runResurfaceShow(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	rcfg := resurfacingCfg()

	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		limit = rcfg.MaxItems
	}
	if limit <= 0 {
		limit = 10
	}

	minScore, _ := cmd.Flags().GetFloat64("min-score")
	if minScore <= 0 {
		minScore = rcfg.MinScore
	}

	profileName := activeProfileName()

	entries, err := svc.ListResurfacing(ctx, profileName, limit, minScore)
	if err != nil {
		return fmt.Errorf("list resurfacing: %w", err)
	}

	// Mark as surfaced.
	for _, e := range entries {
		_ = svc.MarkResurfaced(ctx, e.ID)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"profile": profileName,
			"items":   entries,
			"total":   len(entries),
		})
	}

	if len(entries) == 0 {
		fmt.Println("No resurfacing candidates.")
		if profileName == "" {
			fmt.Println("Tip: set a default profile with: ctxt profile set <name>")
			fmt.Println("     Then run: ctxt resurface refresh")
		}
		return nil
	}

	fmt.Printf("Resurfacing candidates for profile %q (%d)\n\n", profileName, len(entries))
	headers := []string{"Entry ID", "Object ID", "Score", "Reason"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			truncate(e.ID, 12),
			truncate(e.ObjectID, 16),
			fmt.Sprintf("%.2f", e.Score),
			e.Reason,
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runResurfaceRefresh(_ *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	rcfg := resurfacingCfg()

	profileName, profile, ok := activeProfile()
	if !ok && profileName != "" {
		return fmt.Errorf("active profile %q not found in config", profileName)
	}

	job := resurfacing.NewJob(svc.Store, rcfg)
	if err := job.RunOnce(ctx, profileName, profile); err != nil {
		return fmt.Errorf("resurfacing refresh: %w", err)
	}

	fmt.Fprintf(os.Stdout, "Resurfacing queue refreshed for profile %q\n", profileName)
	return nil
}

func runResurfaceDismiss(_ *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := svc.DismissResurfacing(ctx, args[0]); err != nil {
		return fmt.Errorf("dismiss: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Entry %s dismissed\n", args[0])
	return nil
}
