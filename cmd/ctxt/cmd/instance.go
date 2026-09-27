package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/spf13/cobra"
)

// instanceEntry is one row of `ctxt instance list`: a named server.urls
// entry or a dpkms running on this machine. It never carries a token,
// only whether one is attached.
type instanceEntry struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // "endpoint" or "local"
	URL     string `json:"url"`
	Token   bool   `json:"token"`
	Current bool   `json:"current"`
	PID     int    `json:"pid,omitempty"`
	DB      string `json:"db,omitempty"`
	Uptime  string `json:"uptime,omitempty"`
}

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Select the dpkms instance ctxt talks to",
	Long: `Select which dpkms instance ctxt commands talk to.

An instance is either a named server.urls entry (a URL plus its token,
local or remote) or a dpkms running on this machine, found through its
pidfile by name or port:

  server:
    token: <default token>
    urls:
      - name: home
        url: https://dpkms.example.ts.net:7700
        token: <token for home>

Every command talks to exactly one instance, resolved in this order:
--server, --instance or CTXT_INSTANCE, the current instance set by
` + "`ctxt instance use`" + `, the first server.urls entry, server.url, then
http://127.0.0.1:8080. An unknown or stale name exits 70; ctxt never
falls back to another instance.

The current instance is stored in:
  $XDG_DATA_HOME/contexthelp/run/current-instance

Per-call override (takes precedence over the state file):
  ctxt --instance home status
  CTXT_INSTANCE=home ctxt log`,
}

var instanceListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List named endpoints and running local dpkms instances",
	Long: `List every named server.urls entry and every dpkms running on this
machine (discovered via pidfiles).

Each row reports the name, kind (endpoint or local), URL and whether a
token is attached; local rows add PID, database and uptime. A leading
"* " marks the instance this invocation resolves to. Tokens are never
printed. Use ctxt instance use <name> to switch.`,
	RunE: runInstanceList,
}

var instanceUseCmd = &cobra.Command{
	Use:   "use <name|port|->",
	Short: "Set the current instance (persisted to state file)",
	Long: `Set the dpkms instance subsequent ctxt commands talk to.

Pass a server.urls entry name (e.g. 'home'), or the name or port of a
dpkms running on this machine (e.g. 'work' or '8081'); a local instance
is stored by name. Pass '-' to clear the selection. A name that matches
neither exits 70 and lists what is configured.

Examples:
  ctxt instance use home
  ctxt instance use 8081
  ctxt instance use -`,
	Args: cobra.ExactArgs(1),
	RunE: runInstanceUse,
}

var instanceCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Print the instance this invocation talks to",
	Long: `Print the dpkms instance ctxt resolves to: its name, URL, the layer
that chose it (--server, --instance, CTXT_INSTANCE, current-instance,
server.urls, server.url or default) and whether a token is attached. The
token itself is never printed. A stale current-instance selection exits
70.`,
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
		{Title: "List named endpoints and running dpkms instances", Command: "ctxt instance list"},
		{Title: "Alias", Command: "ctxt instance ls"},
	})
	cliconv.WithExamples(instanceCurrentCmd, []cliconv.Example{
		{Title: "Print the active instance", Command: "ctxt instance current"},
		{Title: "JSON for scripting", Command: "ctxt instance current --format json"},
	})
	cliconv.WithExamples(instanceUseCmd, []cliconv.Example{
		{Title: "Switch to a named endpoint", Command: "ctxt instance use home"},
		{Title: "Clear the selection", Command: "ctxt instance use -"},
	})
	cliconv.WithNextSteps(instanceUseCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt instance current", Reason: "confirm the active instance landed on the expected target"},
	})
}

func runInstanceList(cmd *cobra.Command, _ []string) error {
	var sc config.ServerConfig
	if cfg != nil {
		sc = cfg.Server
	}
	locals, err := localInstances()
	if err != nil {
		return fmt.Errorf("list local dpkms instances: %w", err)
	}
	fixed := func() ([]pidfile.Info, error) { return locals, nil }

	// The marker shows where this invocation goes; a stale selection
	// still lists, with a warning instead of a marker.
	current, curErr := resolveEndpoint(cmd)
	if curErr != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", curErr)
	}

	entries := make([]instanceEntry, 0, len(sc.URLs)+len(locals))
	for _, e := range sc.URLs {
		if e.Name == "" {
			continue
		}
		r, err := dpkmsclient.Resolve(sc, dpkmsclient.Selection{Instance: e.Name}, fixed)
		if err != nil {
			return err
		}
		entries = append(entries, instanceEntry{
			Name: e.Name, Kind: "endpoint", URL: e.URL, Token: r.Token != "",
			Current: curErr == nil && !current.Local && current.Name == e.Name,
		})
	}
	for _, info := range locals {
		r, err := dpkmsclient.Resolve(sc, dpkmsclient.Selection{Instance: strconv.Itoa(info.Port)}, fixed)
		if err != nil {
			return err
		}
		entries = append(entries, instanceEntry{
			Name: info.Name, Kind: "local", URL: r.URL, Token: r.Token != "",
			Current: curErr == nil && current.Local && current.Name == info.Name,
			PID:     info.PID, DB: info.DBPath,
			Uptime: time.Since(info.StartedAt).Truncate(time.Second).String(),
		})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), entries)
	}
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No named endpoints (server.urls) and no running dpkms instances.")
		return nil
	}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		marker := "  "
		if e.Current {
			marker = "* "
		}
		pid := ""
		if e.PID != 0 {
			pid = strconv.Itoa(e.PID)
		}
		rows[i] = []string{marker + e.Name, e.Kind, e.URL, yesNo(e.Token), pid, e.DB, e.Uptime}
	}
	printTable(cmd.OutOrStdout(), []string{"NAME", "KIND", "URL", "TOKEN", "PID", "DB", "UPTIME"}, rows)
	return nil
}

func runInstanceUse(cmd *cobra.Command, args []string) error {
	target := strings.TrimSpace(args[0])

	stateFile, err := config.CurrentInstanceFile()
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}

	// '-' clears the selection.
	if target == "-" {
		if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("clear instance: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Current instance cleared.")
		return nil
	}

	var sc config.ServerConfig
	if cfg != nil {
		sc = cfg.Server
	}
	r, err := dpkmsclient.Resolve(sc, dpkmsclient.Selection{Instance: target}, localInstances)
	if err != nil {
		return err
	}

	// A local instance is stored by name, so the selection survives a
	// port reassignment.
	if err := os.WriteFile(stateFile, []byte(r.Name), 0o600); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Current instance set to %q (%s, token: %s)\n", r.Name, r.URL, yesNo(r.Token != ""))
	return nil
}

// instanceCurrent is `ctxt instance current --format json`.
type instanceCurrent struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Key   string `json:"key"`
	Layer string `json:"layer"`
	Token bool   `json:"token"`
	Local bool   `json:"local"`
}

func runInstanceCurrent(cmd *cobra.Command, _ []string) error {
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return err
	}
	cur := instanceCurrent{
		Name: r.Name, URL: r.URL, Key: r.Key, Layer: string(r.Layer),
		Token: r.Token != "", Local: r.Local,
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), cur)
	}
	w := cmd.OutOrStdout()
	name := cur.Name
	if name == "" {
		name = "(unnamed)"
	}
	fmt.Fprintf(w, "%s  %s\n", name, cur.URL)
	fmt.Fprintf(w, "  layer: %s\n", cur.Layer)
	fmt.Fprintf(w, "  token: %s\n", yesNo(cur.Token))
	if cur.Local {
		fmt.Fprintln(w, "  local: running on this machine")
	}
	return nil
}
