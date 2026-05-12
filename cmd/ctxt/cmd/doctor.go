package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run automated health checks on the knowledge graph",
	Long: `Inspect the knowledge graph for common issues: orphaned objects,
missing metadata enrichment, near-duplicates, and stale objects.

Examples:
  ctxt doctor
  ctxt doctor --check orphans,duplicates
  ctxt doctor --format json
  ctxt doctor --profile work --limit 50`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	cliconv.WithSideEffect(doctorCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(doctorCmd, []cliconv.Example{
		{Title: "Run all health checks", Command: "ctxt doctor"},
		{Title: "Limit to specific checks", Command: "ctxt doctor --check orphans,duplicates"},
		{Title: "JSON output for automation", Command: "ctxt doctor --format json"},
	})
	// "doctor" is in kit's defaultIdempotency table (yes); the lint
	// report-only path matches the verb default, so no override needed.
	doctorCmd.Flags().StringSlice("check", nil,
		"checks to run (orphans,missing_metadata,duplicates,stale); default: all")
	// --profile is inherited from kit's persistent flag set; do not
	// re-register it locally. doctor reads it via cmd.Flags().GetString("profile")
	// at run time (cobra resolves inherited persistent flags through Flags()).
	doctorCmd.Flags().Int("limit", 0, "max issues to report (0 = unlimited)")
	doctorCmd.Flags().Int("stale-days", 90, "days without update before flagging as stale")
	doctorCmd.Flags().Float64("duplicate-threshold", 0.95,
		"cosine similarity threshold for near-duplicate detection")
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	checks, _ := cmd.Flags().GetStringSlice("check")
	profile, _ := cmd.Flags().GetString("profile")
	limit, _ := cmd.Flags().GetInt("limit")
	staleDays, _ := cmd.Flags().GetInt("stale-days")
	dupThresh, _ := cmd.Flags().GetFloat64("duplicate-threshold")

	lcfg := lint.DefaultConfig()
	lcfg.Checks = checks
	lcfg.ProfileID = profile
	lcfg.Limit = limit
	lcfg.StaleDays = staleDays
	lcfg.DuplicateThresh = dupThresh

	linter := lint.New(svc.Store, lcfg)
	ctx := context.Background()

	report, err := linter.Run(ctx)
	if err != nil {
		return fmt.Errorf("lint: %w", err)
	}

	// Log report to audit.
	logLintReport(ctx, svc.Store, report)

	if isJSONOutput() {
		return outputJSON(os.Stdout, report)
	}
	printLintReport(report)
	return nil
}

func printLintReport(r *lint.Report) {
	counts := r.Counts()
	fmt.Fprintf(os.Stdout, "Lint checks: %s  (%s)\n\n",
		strings.Join(r.Checks, ", "), r.Duration)

	if len(r.Issues) == 0 {
		fmt.Fprintln(os.Stdout, "No issues found.")
		return
	}

	headers := []string{"Severity", "Check", "Object", "Message"}
	var rows [][]string
	for _, iss := range r.Issues {
		rows = append(rows, []string{
			string(iss.Severity),
			iss.Check,
			iss.ObjectID,
			truncate(iss.Message, 60),
		})
	}
	printTable(os.Stdout, headers, rows)

	fmt.Fprintf(os.Stdout, "\nTotal: %d  (error: %d  warning: %d  info: %d)\n",
		len(r.Issues),
		counts[lint.SeverityError],
		counts[lint.SeverityWarning],
		counts[lint.SeverityInfo],
	)
}

func logLintReport(ctx context.Context, driver storage.StorageDriver, r *lint.Report) {
	payload := map[string]any{
		"checks":   r.Checks,
		"issues":   len(r.Issues),
		"duration": r.Duration,
		"counts":   r.Counts(),
	}
	entry := &storage.AuditEntry{
		ID:        fmt.Sprintf("lint-%d", time.Now().UnixNano()),
		EventType: "doctor.run",
		Actor:     "ctxt-cli",
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	// best-effort; don't fail the command on audit write error
	_ = driver.AuditLog().Append(ctx, entry)
}

