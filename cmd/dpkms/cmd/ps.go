package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

type psRow struct {
	Name   string `table:"NAME"`
	PID    int    `table:"PID"`
	Port   int    `table:"PORT"`
	GRPC   int    `table:"GRPC"`
	DB     string `table:"DB"`
	Uptime string `table:"UPTIME"`
}

type psBrowserRow struct {
	Name    string `table:"NAME"`
	PID     int    `table:"PID"`
	Port    int    `table:"PORT"`
	GRPC    int    `table:"GRPC"`
	Browser string `table:"BROWSER"`
	DB      string `table:"DB"`
	Uptime  string `table:"UPTIME"`
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running dpkms instances",
	Long: `List all dpkms processes running on this machine.

Scans $XDG_DATA_HOME/contexthelp/run/ for pidfiles and validates each
process is still alive. Stale entries (from crashes or reboots) are
removed automatically.

Examples:
  dpkms ps
  dpkms ps --format json`,
	RunE: runPS,
}

func init() {
	rootCmd.AddCommand(psCmd)
	// No local --output here. kit/cli registers --output (-o) on the
	// root as the output-PATH flag; the local redefinition shadowed it
	// with format semantics, so `--output json` and `--format json`
	// disagreed on the same command. --format is the single output-form
	// flag; -o keeps kit's path meaning.

	// Scans pidfiles and reports. The stale-entry sweep only removes
	// pidfiles whose process is already gone, which is bookkeeping over
	// dead state rather than an observable mutation. Read.
	cliconv.WithSideEffect(psCmd, cliconv.SideEffectRead)
}

func runPS(cmd *cobra.Command, _ []string) error {
	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}

	instances, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	// A structured caller gets the scan result whether or not anything
	// is running: an empty list is the answer, not a reason to fall
	// back to the human "nothing here" line.
	//
	// The list travels inside a keyed envelope rather than as a bare
	// array. A top-level array cannot grow a sibling field — a count, a
	// scan root, a warning about a pidfile that could not be read —
	// without changing the document's type under every existing caller,
	// and it leaves the reader nothing to key on to confirm it is
	// looking at a fleet listing at all.
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"instances": instances,
		})
	}

	// Check if any instance has browser enabled. The wider row shape is
	// chosen once, up front, so every format projects the same columns.
	hasBrowser := false
	for _, info := range instances {
		if info.BrowserPort > 0 {
			hasBrowser = true
			break
		}
	}

	// csv/text/table all project from the `table:""` tags above, via
	// kit's registry, so asking for one of them yields that format
	// rather than the human table it used to silently fall back to.
	if cliformat.Rows() {
		if hasBrowser {
			return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), psBrowserRows(instances))
		}
		return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), psRows(instances))
	}

	// human: the bespoke view, unchanged — including the sentence that
	// says nothing is running, which reads better than a blank table.
	if len(instances) == 0 {
		fmt.Println("No running dpkms instances.")
		return nil
	}

	style := output.WithTableStyle(root.TableStyle())
	if hasBrowser {
		return output.Render(os.Stdout, output.Table, psBrowserRows(instances), style)
	}
	return output.Render(os.Stdout, output.Table, psRows(instances), style)
}

// psRows projects scanned instances onto the narrow row shape.
func psRows(instances []pidfile.Info) []psRow {
	rows := make([]psRow, len(instances))
	for i, info := range instances {
		rows[i] = psRow{
			Name:   info.Name,
			PID:    info.PID,
			Port:   info.Port,
			GRPC:   info.GRPCPort,
			DB:     info.DBPath,
			Uptime: time.Since(info.StartedAt).Truncate(time.Second).String(),
		}
	}
	return rows
}

// psBrowserRows projects scanned instances onto the row shape that
// carries the browser port column.
func psBrowserRows(instances []pidfile.Info) []psBrowserRow {
	rows := make([]psBrowserRow, len(instances))
	for i, info := range instances {
		browser := "-"
		if info.BrowserPort > 0 {
			browser = fmt.Sprintf("%d", info.BrowserPort)
		}
		rows[i] = psBrowserRow{
			Name:    info.Name,
			PID:     info.PID,
			Port:    info.Port,
			GRPC:    info.GRPCPort,
			Browser: browser,
			DB:      info.DBPath,
			Uptime:  time.Since(info.StartedAt).Truncate(time.Second).String(),
		}
	}
	return rows
}
