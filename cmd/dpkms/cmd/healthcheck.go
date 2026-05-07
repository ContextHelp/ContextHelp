// Package cmd: dpkms healthcheck — operator-facing healthcheck called
// directly on the dpkms host (e.g. from launchd or systemd). Identical
// semantics to `ctxt status` but available without the user-facing CLI
// installed.
//
// Exit codes:
//
//	0 — server returned 200 + healthy or degraded
//	1 — server unreachable, returned 503, or response could not be parsed
//
// The envelope schema is defined in internal/server/http/handlers_healthz.go.
package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

var healthcheckCmd = &cobra.Command{
	Use:   "healthcheck",
	Short: "Probe a running dpkms instance via /healthz",
	Long: `Calls GET /healthz on the configured dpkms instance and renders the
envelope as a table. With --output json the daemon response is passed
through unchanged. Designed for launchd/systemd liveness probes:

  ProgramArguments: [dpkms, healthcheck, --quiet]

Exit codes:
  0   server reported healthy or degraded (still serving)
  1   server unreachable, returned 503, or response could not be parsed

Examples:
  dpkms healthcheck
  dpkms healthcheck --output json
  dpkms healthcheck --watch                 # refresh every 2 seconds
  dpkms healthcheck --watch --interval 5    # refresh every 5 seconds`,
	RunE: runHealthcheck,
}

func init() {
	rootCmd.AddCommand(healthcheckCmd)
	healthcheckCmd.Flags().Bool("watch", false, "refresh every --interval seconds until interrupted")
	healthcheckCmd.Flags().Int("interval", 2, "seconds between refreshes when --watch is set")
	healthcheckCmd.Flags().Bool("quiet", false, "suppress output; rely on exit code only")
}

type healthcheckRow struct {
	Component string `table:"COMPONENT"`
	Status    string `table:"STATUS"`
}

func runHealthcheck(cmd *cobra.Command, _ []string) error {
	serverURL := viper.GetString("server.url")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetInt("interval")
	if interval < 1 {
		interval = 2
	}
	quiet, _ := cmd.Flags().GetBool("quiet")

	if !watch {
		return healthcheckOnce(cmd, serverURL, quiet)
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		fmt.Fprint(cmd.OutOrStdout(), "\033[H\033[2J")
		_ = healthcheckOnce(cmd, serverURL, quiet)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func healthcheckOnce(cmd *cobra.Command, serverURL string, quiet bool) error {
	client := NewAPIClient(serverURL)
	env, status, err := client.Healthz()
	if err != nil {
		if !quiet {
			fmt.Fprintf(cmd.ErrOrStderr(), "healthcheck %s: %v\n", serverURL, err)
		}
		return fmt.Errorf("healthcheck %s: %w", serverURL, err)
	}

	if !quiet {
		if viper.GetString("output.format") == "json" {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			if err := enc.Encode(env); err != nil {
				return err
			}
		} else {
			renderHealthcheck(cmd, env, serverURL)
		}
	}

	if status == gohttp.StatusServiceUnavailable || env.Health == "failed" {
		return fmt.Errorf("dpkms health: %s", env.Health)
	}
	return nil
}

// renderHealthcheck draws the table view via kit/console/output to match
// the styling of `dpkms ps`.
func renderHealthcheck(cmd *cobra.Command, env HealthzEnvelope, serverURL string) {
	w := cmd.OutOrStdout()
	uptime := time.Duration(env.UptimeSeconds) * time.Second
	fmt.Fprintf(w, "dpkms %s — %s (uptime %s)\n",
		serverURL, strings.ToUpper(env.Health), uptime.Truncate(time.Second).String())
	if env.Version != "" {
		fmt.Fprintf(w, "version: %s\n", env.Version)
	}
	fmt.Fprintln(w)

	rows := []healthcheckRow{
		{Component: "process", Status: env.Checks.Process},
		{Component: "rest_api", Status: env.Checks.RESTAPI},
		{Component: "grpc_api", Status: env.Checks.GRPCAPI},
		{Component: "db", Status: env.Checks.DB.Status},
		{Component: "queue", Status: fmt.Sprintf("pending=%d running=%d failed=%d",
			env.Checks.Queue.Pending, env.Checks.Queue.Running, env.Checks.Queue.Failed)},
		{Component: "watchers", Status: fmt.Sprintf("%d registered", len(env.Checks.Watchers))},
	}

	style := output.WithTableStyle(root.TableStyle())
	_ = output.Render(w, output.Table, rows, style)

	if len(env.Upgrade) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "upgrade in progress:")
		for k, v := range env.Upgrade {
			fmt.Fprintf(w, "  %-12s %v\n", k, v)
		}
	}
}

// keep os imported (used implicitly through cobra runtime); avoids
// drift if tests need to swap stdout later.
var _ = os.Stdout
