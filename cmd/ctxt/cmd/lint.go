package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lint"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Run automated health checks on the knowledge graph",
	Long: `Inspect the knowledge graph for common issues: orphaned objects,
missing metadata enrichment, near-duplicates, and stale objects.

Examples:
  ctxt lint
  ctxt lint --check orphans,duplicates
  ctxt lint --output json
  ctxt lint --profile work --limit 50`,
	RunE: runLint,
}

func init() {
	rootCmd.AddCommand(lintCmd)
	lintCmd.Flags().StringSlice("check", nil,
		"checks to run (orphans,missing_metadata,duplicates,stale); default: all")
	lintCmd.Flags().String("profile", "", "scope checks to a focus profile")
	lintCmd.Flags().Int("limit", 0, "max issues to report (0 = unlimited)")
	lintCmd.Flags().Int("stale-days", 90, "days without update before flagging as stale")
	lintCmd.Flags().Float64("duplicate-threshold", 0.95,
		"cosine similarity threshold for near-duplicate detection")
}

func runLint(cmd *cobra.Command, _ []string) error {
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
		EventType: "lint.run",
		Actor:     "ctxt-cli",
		Payload:   payload,
		CreatedAt: time.Now(),
	}
	// best-effort; don't fail the command on audit write error
	_ = driver.AuditLog().Append(ctx, entry)
}

