package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PID\tPORT\tGRPC\tDB\tUPTIME")
	for _, info := range instances {
		uptime := time.Since(info.StartedAt).Truncate(time.Second)
		fmt.Fprintf(w, "%d\t%d\t%d\t%s\t%s\n",
			info.PID, info.Port, info.GRPCPort, info.DBPath, uptime)
	}
	return w.Flush()
}
