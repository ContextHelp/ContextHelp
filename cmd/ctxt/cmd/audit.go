package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/audit"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Audit log management",
	Long:  `Inspect and export the append-only audit log.`,
}

var auditListCmd = &cobra.Command{
	Use:   "list",
	Short: "List audit log entries",
	RunE:  runAuditList,
}

var auditExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export audit log entries to stdout",
	Long: `Export audit log entries in a SIEM-compatible format.

Formats:
  json    NDJSON — one JSON object per line
  cef     ArcSight Common Event Format v0
  syslog  RFC 5424 structured-data text (one record per line)

Examples:
  ctxt audit export --format json
  ctxt audit export --format cef --since 24h
  ctxt audit export --format syslog --actor alice@example.com`,
	RunE: runAuditExport,
}

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.AddCommand(auditListCmd)
	auditCmd.AddCommand(auditExportCmd)

	// Shared filter flags.
	for _, cmd := range []*cobra.Command{auditListCmd, auditExportCmd} {
		cmd.Flags().String("actor", "", "filter by actor (principal)")
		cmd.Flags().String("event-type", "", "filter by event type (e.g. create, delete)")
		cmd.Flags().String("object-id", "", "filter by object ID")
		cmd.Flags().String("since", "", "filter entries after duration ago (e.g. 24h, 7d)")
		cmd.Flags().Int("limit", 100, "max entries to return (0 = unlimited)")
		cmd.Flags().Int("offset", 0, "pagination offset")

		viper.BindPFlag("audit.list.actor", cmd.Flags().Lookup("actor"))
		viper.BindPFlag("audit.list.event_type", cmd.Flags().Lookup("event-type"))
		viper.BindPFlag("audit.list.object_id", cmd.Flags().Lookup("object-id"))
		viper.BindPFlag("audit.list.since", cmd.Flags().Lookup("since"))
		viper.BindPFlag("audit.list.limit", cmd.Flags().Lookup("limit"))
		viper.BindPFlag("audit.list.offset", cmd.Flags().Lookup("offset"))
	}

	// Export-only flags.
	auditExportCmd.Flags().String("format", "json", "output format: json | cef | syslog")
	viper.BindPFlag("audit.export.format", auditExportCmd.Flags().Lookup("format"))
}

// buildAuditFilter constructs an AuditFilter from CLI flags / viper bindings.
func buildAuditFilter(cmd *cobra.Command) (storage.AuditFilter, error) {
	f := storage.AuditFilter{
		Actor:     mustGetString(cmd, "actor"),
		EventType: mustGetString(cmd, "event-type"),
		ObjectID:  mustGetString(cmd, "object-id"),
		Limit:     mustGetInt(cmd, "limit"),
		Offset:    mustGetInt(cmd, "offset"),
	}
	if since := mustGetString(cmd, "since"); since != "" {
		d, err := parseDuration(since)
		if err != nil {
			return f, fmt.Errorf("--since: %w", err)
		}
		f.After = time.Now().Add(-d)
	}
	return f, nil
}

func runAuditList(cmd *cobra.Command, _ []string) error {
	filter, err := buildAuditFilter(cmd)
	if err != nil {
		return err
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	entries, total, err := svc.Store.AuditLog().List(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("audit list: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"total":   total,
			"entries": entries,
		})
	}

	headers := []string{"ID", "EVENT", "OBJECT", "ACTOR", "TIME"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []string{
			e.ID,
			e.EventType,
			e.ObjectID,
			e.Actor,
			e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	printTable(os.Stdout, headers, rows)
	fmt.Fprintf(os.Stdout, "Total: %d\n", total)
	return nil
}

func runAuditExport(cmd *cobra.Command, _ []string) error {
	fmtStr := mustGetString(cmd, "format")
	if fmtStr == "" {
		fmtStr = "json"
	}
	fmt_, err := audit.ParseFormat(fmtStr)
	if err != nil {
		return err
	}

	filter, err := buildAuditFilter(cmd)
	if err != nil {
		return err
	}
	// Unlimited by default for export.
	if filter.Limit == 100 {
		filter.Limit = 0
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	entries, _, err := svc.Store.AuditLog().List(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("audit export: %w", err)
	}

	return writeExport(os.Stdout, entries, fmt_)
}

// writeExport serialises entries in the requested format to w, one record per line.
func writeExport(w io.Writer, entries []*storage.AuditEntry, f audit.Format) error {
	for _, e := range entries {
		line, err := audit.Marshal(e, f)
		if err != nil {
			return fmt.Errorf("audit marshal: %w", err)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// mustGetString retrieves a string flag value; returns "" on error (flag not set).
func mustGetString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

// mustGetInt retrieves an int flag value; returns 0 on error.
func mustGetInt(cmd *cobra.Command, name string) int {
	v, _ := cmd.Flags().GetInt(name)
	return v
}

// parseDuration extends time.ParseDuration with "d" (day) suffix support.
func parseDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := time.ParseDuration(s[:len(s)-1] + "h")
		if err != nil {
			return 0, err
		}
		return days * 24, nil
	}
	return time.ParseDuration(s)
}
