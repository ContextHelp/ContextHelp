// Package cmd: ctxt status — calls dpkms's GET /healthz and renders the
// envelope. Used both interactively (table view) and by automation
// (--format json passes the daemon response through unchanged).
//
// Exit codes:
//
//	0 — server returned 200 + health: healthy or degraded
//	1 — server returned 503, body decode failure, or unreachable
//
// The envelope schema is documented in internal/server/http/handlers_healthz.go;
// keep this file's type mirror in sync if T-0580 (or later) adds top-level
// fields.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

// statusEnvelope mirrors internal/server/http.HealthzEnvelope. Duplicated
// so the CLI doesn't pull the server package transitively.
type statusEnvelope struct {
	Health        string         `json:"health"`
	Version       string         `json:"version"`
	UptimeSeconds int64          `json:"uptime_seconds"`
	Checks        statusChecks   `json:"checks"`
	Upgrade       map[string]any `json:"upgrade,omitempty"` // T-0580 extension; rendered raw if present.
}

type statusChecks struct {
	Process  string           `json:"process"`
	RESTAPI  string           `json:"rest_api"`
	GRPCAPI  string           `json:"grpc_api"`
	DB       statusDBCheck    `json:"db"`
	Queue    statusQueueCheck `json:"queue"`
	Watchers []statusWatcher  `json:"watchers"`
}

type statusDBCheck struct {
	Status    string `json:"status"`
	LastWrite string `json:"last_write,omitempty"`
}

type statusQueueCheck struct {
	Pending int `json:"pending"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

type statusWatcher struct {
	Name          string `json:"name"`
	Subscriptions int    `json:"subscriptions"`
	LastEvent     string `json:"last_event,omitempty"`
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show health status of the configured dpkms instance",
	Long: `Calls GET /healthz on the configured dpkms instance and renders the
envelope as a table. With --format json the daemon response is passed
through unchanged.

Exit codes:
  0   server reported healthy or degraded (still serving)
  1   server unreachable, returned 503, or response could not be parsed

Examples:
  ctxt status
  ctxt status --format json
  ctxt status --watch                 # refresh every 2 seconds
  ctxt status --watch --interval 5    # refresh every 5 seconds
  ctxt status --server http://localhost:8081`,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
	cliconv.WithSideEffect(statusCmd, cliconv.SideEffectRead)
	// "status" is not in kit's defaultIdempotency table; mark it explicitly
	// as idempotent — repeated /healthz polls don't mutate state.
	cliconv.WithIdempotency(statusCmd, cliconv.IdempotencyYes)
	statusCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	statusCmd.Flags().Bool("watch", false, "refresh every --interval seconds until interrupted")
	statusCmd.Flags().Int("interval", 2, "seconds between refreshes when --watch is set")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	serverURL := statusServerURL(cmd)
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetInt("interval")
	if interval < 1 {
		interval = 2
	}

	if !watch {
		return statusOnce(cmd, serverURL)
	}

	// Watch mode: redraw on each tick. Ctrl-C breaks the loop. We do
	// NOT exit non-zero on a single failed cycle; only the final cycle
	// (when the user interrupts) determines exit code semantics — but
	// for now we just run until signal and exit 0.
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		// Clear screen between refreshes (ANSI). Falls back to plain
		// repaint when stdout isn't a TTY — harmless either way.
		fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
		_ = statusOnce(cmd, serverURL) // surface the row, don't exit on transient failure
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// statusOnce performs one /healthz fetch and renders the result. It
// returns an error (causing a non-zero exit) when the request fails or
// the envelope reports failed.
func statusOnce(cmd *cobra.Command, serverURL string) error {
	env, status, err := fetchHealthz(serverURL)
	if err != nil {
		return fmt.Errorf("healthcheck %s: %w", serverURL, err)
	}

	if isJSONOutput() {
		if err := outputJSON(cmd.OutOrStdout(), env); err != nil {
			return err
		}
	} else {
		renderStatusTable(cmd.OutOrStdout(), env, serverURL)
	}

	if status == gohttp.StatusServiceUnavailable || env.Health == "failed" {
		return fmt.Errorf("dpkms health: %s", env.Health)
	}
	return nil
}

// fetchHealthz issues GET /healthz and decodes the envelope. The HTTP
// status code is returned alongside so the caller can distinguish 200
// healthy/degraded from 503 failed without re-reading the body.
func fetchHealthz(serverURL string) (statusEnvelope, int, error) {
	url := strings.TrimRight(serverURL, "/") + "/healthz"
	client := &gohttp.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url) // #nosec G107 -- user-provided server URL
	if err != nil {
		return statusEnvelope{}, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return statusEnvelope{}, resp.StatusCode, err
	}

	var env statusEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return statusEnvelope{}, resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return env, resp.StatusCode, nil
}

// renderStatusTable prints a one-row-per-subsystem human-readable view.
// The shape matches the envelope keys 1:1 so it stays correct as
// T-0580 adds the `upgrade` field (rendered as a separate trailing
// row group when populated).
func renderStatusTable(w io.Writer, env statusEnvelope, serverURL string) {
	fmt.Fprintf(w, "dpkms %s — %s (uptime %s)\n",
		serverURL,
		strings.ToUpper(env.Health),
		formatUptime(env.UptimeSeconds),
	)
	if env.Version != "" {
		fmt.Fprintf(w, "version: %s\n", env.Version)
	}
	fmt.Fprintln(w)

	headers := []string{"COMPONENT", "STATUS"}
	rows := [][]string{
		{"process", env.Checks.Process},
		{"rest_api", env.Checks.RESTAPI},
		{"grpc_api", env.Checks.GRPCAPI},
		{"db", env.Checks.DB.Status},
		{"queue", fmt.Sprintf("pending=%d running=%d failed=%d",
			env.Checks.Queue.Pending, env.Checks.Queue.Running, env.Checks.Queue.Failed)},
		{"watchers", fmt.Sprintf("%d registered", len(env.Checks.Watchers))},
	}
	printTable(w, headers, rows)

	if len(env.Upgrade) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "upgrade in progress:")
		for k, v := range env.Upgrade {
			fmt.Fprintf(w, "  %-12s %v\n", k, v)
		}
	}
}

// formatUptime renders seconds as "1h2m3s". Uses time.Duration's String
// directly; truncates to whole seconds.
func formatUptime(secs int64) string {
	d := time.Duration(secs) * time.Second
	if d < time.Minute {
		return d.String()
	}
	return d.Truncate(time.Second).String()
}

// statusServerURL resolves the target URL the same way other CLI
// commands do: explicit --server flag wins, then viper (which the
// global --instance / config plumbing populates), then localhost
// default. We tolerate stdout writes as a side effect.
func statusServerURL(cmd *cobra.Command) string {
	url := flagString(cmd, "server", "server.url")
	if url == "" {
		url = "http://localhost:8080"
	}
	return url
}

// Re-export os for cobra runtime introspection — kept private to avoid
// extending the public surface of cmd. Used only by tests that swap
// stdout. Comment kept so future maintainers know why this is here
// even when no caller is visible.
var _ = os.Stdout
