package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Query the knowledge changelog",
	Long: `Query the append-only audit log via the dpkms HTTP API.

Lists recent audit log entries with optional filters for event type,
date range, object ID, and actor.

Examples:
  # Recent entries (default limit 20)
  ctxt log

  # Filter by event type (comma-separated)
  ctxt log --type create,enrich

  # Date range
  ctxt log --since 2026-01-01 --until 2026-01-31

  # Object history
  ctxt log --object obj_abc123

  # Filter by actor
  ctxt log --actor system`,
	RunE: runLog,
}

func init() {
	rootCmd.AddCommand(logCmd)
	cliconv.WithSideEffect(logCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(logCmd, []cliconv.Example{
		{Title: "Show recent log entries", Command: "ctxt log"},
		{Title: "Filter by event type", Command: "ctxt log --type create,enrich"},
		{Title: "View history of one object", Command: "ctxt log --object obj_abc123"},
	})
	// "log" is not in kit's defaultIdempotency table; audit-log reads are
	// naturally idempotent — re-querying does not mutate state.
	cliconv.WithIdempotency(logCmd, cliconv.IdempotencyYes)

	logCmd.Flags().Int("limit", 20, "max entries to return")
	logCmd.Flags().String("type", "", "filter by event type (comma-separated)")
	logCmd.Flags().String("since", "", "entries after date (YYYY-MM-DD)")
	logCmd.Flags().String("until", "", "entries before date (YYYY-MM-DD)")
	logCmd.Flags().String("object", "", "filter by object ID")
	logCmd.Flags().String("actor", "", "filter by actor")
	logCmd.Flags().String("server", "",
		"dpkms server URL (default http://localhost:8080)")

	viper.BindPFlag("log.limit", logCmd.Flags().Lookup("limit"))
	viper.BindPFlag("log.type", logCmd.Flags().Lookup("type"))
	viper.BindPFlag("log.since", logCmd.Flags().Lookup("since"))
	viper.BindPFlag("log.until", logCmd.Flags().Lookup("until"))
	viper.BindPFlag("log.object", logCmd.Flags().Lookup("object"))
	viper.BindPFlag("log.actor", logCmd.Flags().Lookup("actor"))
}

// logResponse matches the JSON envelope from GET /api/v1/audit-log.
type logResponse struct {
	Data  []*storage.AuditEntry `json:"data"`
	Total int                   `json:"total"`
}

func runLog(cmd *cobra.Command, _ []string) error {
	serverURL := flagString(cmd, "server", "server.url")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	params := url.Values{}
	if v := mustGetInt(cmd, "limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v := mustGetString(cmd, "type"); v != "" {
		params.Set("type", v)
	}
	if v := mustGetString(cmd, "since"); v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return fmt.Errorf("--since: expected YYYY-MM-DD: %w", err)
		}
		params.Set("since", v)
	}
	if v := mustGetString(cmd, "until"); v != "" {
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return fmt.Errorf("--until: expected YYYY-MM-DD: %w", err)
		}
		params.Set("until", v)
	}
	if v := mustGetString(cmd, "object"); v != "" {
		params.Set("object_id", v)
	}
	if v := mustGetString(cmd, "actor"); v != "" {
		params.Set("actor", v)
	}

	endpoint := serverURL + "/api/v1/audit-log?" + params.Encode()
	resp, err := gohttp.Get(endpoint) // #nosec G107 -- user-provided server URL
	if err != nil {
		return fmt.Errorf("audit-log request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != gohttp.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, body)
	}

	var result logResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, result)
	}

	fmt.Printf("Audit Log (%d total)\n\n", result.Total)
	headers := []string{"TIME", "EVENT", "OBJECT", "ACTOR", "DELTA"}
	rows := make([][]string, 0, len(result.Data))
	for _, e := range result.Data {
		delta := formatPayload(e.Payload)
		rows = append(rows, []string{
			e.CreatedAt.UTC().Format("2006-01-02 15:04:05"),
			e.EventType,
			truncate(e.ObjectID, 20),
			e.Actor,
			truncate(delta, 40),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

// formatPayload renders an audit entry payload as a compact string.
func formatPayload(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Sprintf("%v", p)
	}
	return string(b)
}
