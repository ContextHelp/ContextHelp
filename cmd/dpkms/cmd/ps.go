package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

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
  dpkms ps --output json`,
	RunE: runPS,
}

func init() {
	rootCmd.AddCommand(psCmd)
	psCmd.Flags().StringP("output", "o", "", "output format (json)")
}

func runPS(cmd *cobra.Command, _ []string) error {
	outFmt, _ := cmd.Flags().GetString("output")

	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}

	instances, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	if outFmt == "json" {
		return json.NewEncoder(os.Stdout).Encode(instances)
	}

	if len(instances) == 0 {
		fmt.Println("No running dpkms instances.")
		return nil
	}

	// Check if any instance has browser enabled.
	hasBrowser := false
	for _, info := range instances {
		if info.BrowserPort > 0 {
			hasBrowser = true
			break
		}
	}

	style := output.WithTableStyle(root.TableStyle())
	if hasBrowser {
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
		return output.Render(os.Stdout, output.Table, rows, style)
	}
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
	return output.Render(os.Stdout, output.Table, rows, style)
}
