package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

type instanceRow struct {
	Name   string `table:"NAME"`
	PID    int    `table:"PID"`
	Port   int    `table:"PORT"`
	GRPC   int    `table:"GRPC"`
	DB     string `table:"DB"`
	Uptime string `table:"UPTIME"`
}

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage the active dpkms instance",
	Long: `Manage which dpkms instance ctxt commands target.

When multiple dpkms instances are running (each with its own DB), use these
commands to select which one receives ctxt commands.

The active instance is stored in:
  $XDG_DATA_HOME/contexthelp/run/current-instance

Per-call override (takes precedence over the state file):
  ctxt --instance <name> stats
  CTXT_INSTANCE=work ctxt stats`,
}

var instanceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List running dpkms instances",
	Long: `List every running dpkms instance discovered via PID files.

Each row reports name, PID, REST port, gRPC port, DB path, and uptime. A
leading "* " marks the currently active instance (per the state file).
Use ctxt instance use <name> to switch the active instance.`,
	RunE: runInstanceList,
}

var instanceUseCmd = &cobra.Command{
	Use:   "use <name|-|port>",
	Short: "Set the current instance (persisted to state file)",
	Long: `Set the active dpkms instance for all subsequent ctxt commands.

Pass '-' to clear the selection and revert to config-file storage.path.
Pass a name (e.g. 'work') or a port number (e.g. '8081').

Examples:
  ctxt instance use work
  ctxt instance use 8081
  ctxt instance use -`,
	Args: cobra.ExactArgs(1),
	RunE: runInstanceUse,
}

var instanceCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Print the active instance name",
	Long: `Print the name of the currently active dpkms instance.

When a per-call override is set (CTXT_INSTANCE env var or --instance flag),
that takes precedence. Otherwise the state file under
$XDG_DATA_HOME/contexthelp/run/current-instance is consulted. Prints
"(none — using config storage.path)" when no instance is selected.`,
	RunE: runInstanceCurrent,
}

func init() {
	rootCmd.AddCommand(instanceCmd)
	instanceCmd.AddCommand(instanceListCmd)
	instanceCmd.AddCommand(instanceUseCmd)
	instanceCmd.AddCommand(instanceCurrentCmd)

	// 12fcc conformance: side-effect + idempotency annotations.
	// list/current are read-only; use writes the state file
	// (write-local — affects only the current user's ctxt state).
	cliconv.WithSideEffect(instanceListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(instanceCurrentCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(instanceUseCmd, cliconv.SideEffectWriteLocal)
	// Kit verb defaults cover list/current/use (all Yes); no
	// explicit idempotency overrides needed.

	// 12fcc strict-gate: examples + next-steps on every leaf.
	cliconv.WithExamples(instanceListCmd, []cliconv.Example{
		{Title: "List running dpkms instances", Command: "ctxt instance list"},
		{Title: "Alias", Command: "ctxt instance ls"},
	})
	cliconv.WithExamples(instanceCurrentCmd, []cliconv.Example{
		{Title: "Print the active instance", Command: "ctxt instance current"},
		{Title: "JSON for scripting", Command: "ctxt instance current --format json"},
	})
	cliconv.WithExamples(instanceUseCmd, []cliconv.Example{
		{Title: "Switch to a named instance", Command: "ctxt instance use work"},
		{Title: "Clear the selection", Command: "ctxt instance use -"},
	})
	cliconv.WithNextSteps(instanceUseCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt instance current", Reason: "confirm the active instance landed on the expected target"},
	})
}

func runInstanceList(cmd *cobra.Command, _ []string) error {
	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}
	instances, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if len(instances) == 0 {
		fmt.Println("No running dpkms instances.")
		return nil
	}

	current := activeInstanceName()

	rows := make([]instanceRow, len(instances))
	for i, info := range instances {
		marker := "  "
		if info.Name == current || fmt.Sprintf("%d", info.Port) == current {
			marker = "* "
		}
		rows[i] = instanceRow{
			Name:   marker + info.Name,
			PID:    info.PID,
			Port:   info.Port,
			GRPC:   info.GRPCPort,
			DB:     info.DBPath,
			Uptime: time.Since(info.StartedAt).Truncate(time.Second).String(),
		}
	}
	return output.Render(os.Stdout, output.Table, rows, output.WithTableStyle(root.TableStyle()))
}

func runInstanceUse(cmd *cobra.Command, args []string) error {
	target := args[0]

	stateFile, err := config.CurrentInstanceFile()
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}

	// '-' clears the selection.
	if target == "-" {
		if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear instance: %w", err)
		}
		fmt.Println("Current instance cleared. Using config storage.path.")
		return nil
	}

	// Validate: must match a live instance.
	runDir, err := config.RunDir()
	if err != nil {
		return fmt.Errorf("run dir: %w", err)
	}
	infos, err := pidfile.Scan(runDir)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	var found *pidfile.Info
	for i := range infos {
		if infos[i].Name == target || fmt.Sprintf("%d", infos[i].Port) == target {
			found = &infos[i]
			break
		}
	}
	if found == nil {
		e := output.PrerequisiteError(fmt.Sprintf("no running dpkms instance named %q", target))
		e.SuggestedFix = "run `ctxt instance list` (or `dpkms ps`) to see running instances"
		return e
	}

	// Normalise to name so the state file is stable across port reassignments.
	if err := os.WriteFile(stateFile, []byte(found.Name), 0644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	fmt.Printf("Current instance set to %q (port %d, db %s)\n", found.Name, found.Port, found.DBPath)
	return nil
}

func runInstanceCurrent(cmd *cobra.Command, _ []string) error {
	current := activeInstanceName()
	if current == "" {
		runDir, err := config.RunDir()
		if err == nil {
			stateFile, _ := config.CurrentInstanceFile()
			if _, statErr := os.Stat(stateFile); statErr == nil {
				// State file exists but was empty/whitespace after trim — unusual.
				_ = runDir
			}
		}
		fmt.Println("(none — using config storage.path)")
		return nil
	}

	// Resolve to full info if possible.
	runDir, err := config.RunDir()
	if err != nil {
		fmt.Println(current)
		return nil
	}
	infos, err := pidfile.Scan(runDir)
	if err != nil {
		fmt.Println(current)
		return nil
	}
	for _, info := range infos {
		if info.Name == current || fmt.Sprintf("%d", info.Port) == current {
			if isJSONOutput() {
				return outputJSON(os.Stdout, info)
			}
			fmt.Printf("%s (port %d, db %s)\n", info.Name, info.Port, info.DBPath)
			return nil
		}
	}

	// Selected but not running.
	fmt.Printf("%s (not running — use `ctxt instance use -` to clear)\n",
		strings.TrimSpace(current))
	return nil
}
