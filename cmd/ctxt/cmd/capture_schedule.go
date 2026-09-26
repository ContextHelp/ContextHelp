package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

const (
	defaultHistoryEvery = 5 * time.Minute
	defaultTabsEvery    = 30 * time.Minute
	minScheduleEvery    = time.Minute
)

var captureScheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Run browser tab and history capture on a timer (macOS LaunchAgent)",
	Long: `Schedule browser capture with a per-user macOS LaunchAgent.

install writes one agent per capture kind for a browser profile:

  history  runs "ctxt capture history" every 5m (--history-every)
  tabs     runs "ctxt capture tabs" every 30m (--tabs-every)

Each agent is a separate launchd job so each kind keeps its own
interval, log files and failure state. Agents run at login and right
after install. Logs land in ~/Library/Logs/ctxt/.

macOS only. Other platforms are refused.`,
}

var captureScheduleInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install LaunchAgents that capture one browser profile on a timer",
	Long: `Install LaunchAgents that run browser capture for one browser profile.

Writes ~/Library/LaunchAgents/<label>.plist per kind and loads it with
launchctl. Re-running install replaces the agents in place, so it is
also how you change an interval.

--browser and --browser-profile are optional and resolve like
"ctxt capture tabs": capture.browser, then the one installed browser
holding the profile, then the OS default browser; an omitted profile is
the browser's last-used one ("ctxt capture browsers" shows the pick).
Install resolves once and writes the browser and the profile folder
into the agents, so scheduled runs never auto-select. A profile that
matches nothing, or several, is refused (exit 2) with the candidates.

--instance and --profile given here are baked into the scheduled
command line, so the scheduled runs target the same ctxt instance.

--dry-run prints each plist and its target path; nothing is written and
launchctl is not called.`,
	Args: cobra.NoArgs,
	RunE: runCaptureScheduleInstall,
}

var captureScheduleUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Unload and remove the capture LaunchAgents for one browser profile",
	Long: `Unload and remove the capture LaunchAgents for one browser profile.

Removes both kinds unless --no-tabs or --no-history keeps one. Log
files are left in place. Uninstalling a profile with no agents is a
no-op.

--browser and --browser-profile resolve as they do for install. For a
profile since deleted from the browser, pass --browser and the profile
folder shown by "ctxt capture schedule list".`,
	Args: cobra.NoArgs,
	RunE: runCaptureScheduleUninstall,
}

var captureScheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed browser capture LaunchAgents",
	Long: `List the browser capture LaunchAgents in ~/Library/LaunchAgents.

Each row is read back from the agent's plist, so it shows what launchd
will actually run; LOADED reports whether launchd currently has the job.`,
	Args: cobra.NoArgs,
	RunE: runCaptureScheduleList,
}

func init() {
	captureCmd.AddCommand(captureScheduleCmd)
	captureScheduleCmd.AddCommand(captureScheduleInstallCmd, captureScheduleUninstallCmd, captureScheduleListCmd)

	for _, c := range []*cobra.Command{captureScheduleInstallCmd, captureScheduleUninstallCmd} {
		c.Flags().String("browser", "", "browser to capture: "+browserNames()+" (default: capture.browser, else auto-select)")
		c.Flags().String("browser-profile", "", "browser profile display name or folder (e.g. \"Work\", \"Profile 1\"; default: last used)")
		c.Flags().Bool("no-tabs", false, "leave the tabs agent out")
		c.Flags().Bool("no-history", false, "leave the history agent out")
		// Both are always reversible (re-run install / uninstall) and
		// converge on the same state when repeated.
		cliconv.WithSideEffect(c, cliconv.SideEffectWriteLocal)
		cliconv.WithIdempotency(c, cliconv.IdempotencyYes)
	}
	captureScheduleInstallCmd.Flags().Duration("history-every", defaultHistoryEvery, "interval between history captures (min 1m)")
	captureScheduleInstallCmd.Flags().Duration("tabs-every", defaultTabsEvery, "interval between tab snapshots (min 1m)")

	cliconv.WithExamples(captureScheduleInstallCmd, []cliconv.Example{
		{Title: "Capture a Chrome profile on the default timers", Command: `ctxt capture schedule install --browser chrome --browser-profile Work`},
		{Title: "History only, every 10 minutes", Command: `ctxt capture schedule install --browser brave --browser-profile Work --no-tabs --history-every 10m`},
		{Title: "Preview the plists", Command: `ctxt capture schedule install --browser chrome --browser-profile Work --dry-run`},
		{Title: "Whichever browser holds the Work profile", Command: `ctxt capture schedule install --browser-profile Work`},
	})
	cliconv.WithNextSteps(captureScheduleInstallCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt capture schedule list", Reason: "confirm the agents are loaded"},
		{When: "to watch a run", Suggest: "tail -f ~/Library/Logs/ctxt/*.log", Reason: "scheduled output lands there"},
	})

	cliconv.WithExamples(captureScheduleUninstallCmd, []cliconv.Example{
		{Title: "Remove both agents", Command: `ctxt capture schedule uninstall --browser chrome --browser-profile Work`},
		{Title: "Remove only the tabs agent", Command: `ctxt capture schedule uninstall --browser chrome --browser-profile Work --no-history`},
	})
	cliconv.WithNextSteps(captureScheduleUninstallCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt capture schedule list", Reason: "confirm nothing is left behind"},
	})

	cliconv.WithSideEffect(captureScheduleListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(captureScheduleListCmd, []cliconv.Example{
		{Title: "List scheduled capture agents", Command: "ctxt capture schedule list"},
		{Title: "As JSON", Command: "ctxt capture schedule list --format json"},
	})
}

func browserNames() string {
	names := make([]string, 0, len(chromium.Browsers()))
	for _, b := range chromium.Browsers() {
		names = append(names, string(b))
	}
	return strings.Join(names, ", ")
}

// scheduleRow is the result shape for install and list.
type scheduleRow struct {
	Label          string `json:"label" table:"LABEL"`
	Kind           string `json:"kind" table:"KIND"`
	Browser        string `json:"browser" table:"BROWSER"`
	BrowserProfile string `json:"browser_profile" table:"PROFILE"`
	Every          string `json:"every" table:"EVERY"`
	Instance       string `json:"instance,omitempty"`
	FocusProfile   string `json:"profile,omitempty"`
	Plist          string `json:"plist"`
	StdoutLog      string `json:"stdout_log"`
	StderrLog      string `json:"stderr_log"`
	Loaded         bool   `json:"loaded" table:"LOADED"`
}

func newScheduleRow(env *scheduleEnv, a scheduleAgent) scheduleRow {
	return scheduleRow{
		Label:          a.label(),
		Kind:           string(a.Kind),
		Browser:        a.Browser,
		BrowserProfile: a.BrowserProfile,
		Every:          a.Interval.String(),
		Instance:       a.Instance,
		FocusProfile:   a.FocusProfile,
		Plist:          env.plistPath(a.label()),
		StdoutLog:      a.StdoutPath(),
		StderrLog:      a.StderrPath(),
		Loaded:         env.launchctl.Loaded(a.label()),
	}
}

// scheduleEnvForRun resolves the host environment and refuses every
// platform launchd does not exist on.
func scheduleEnvForRun(cmd *cobra.Command) (*scheduleEnv, error) {
	env, err := newScheduleEnv()
	if err != nil {
		return nil, err
	}
	if env.goos != "darwin" {
		return nil, output.GenericError(fmt.Sprintf(
			"%s is unsupported on %s: it installs macOS LaunchAgents only", cmd.CommandPath(), env.goos))
	}
	return env, nil
}

// scheduleKindsFor reads --no-tabs / --no-history.
func scheduleKindsFor(cmd *cobra.Command) ([]scheduleKind, error) {
	noTabs, _ := cmd.Flags().GetBool("no-tabs")
	noHistory, _ := cmd.Flags().GetBool("no-history")
	if noTabs && noHistory {
		return nil, output.UsageError("--no-tabs and --no-history together leave nothing to do")
	}
	kinds := make([]scheduleKind, 0, len(scheduleKinds))
	for _, k := range scheduleKinds {
		if (k == kindTabs && noTabs) || (k == kindHistory && noHistory) {
			continue
		}
		kinds = append(kinds, k)
	}
	return kinds, nil
}

// scheduleTargetFlags returns --browser and --browser-profile,
// rejecting values launchd cannot carry.
func scheduleTargetFlags(cmd *cobra.Command) (browserFlag, profileFlag string, err error) {
	browserFlag, _ = cmd.Flags().GetString("browser")
	profileFlag, _ = cmd.Flags().GetString("browser-profile")
	if cmd.Flags().Changed("browser") && strings.TrimSpace(browserFlag) == "" {
		return "", "", output.UsageError("--browser is blank (" + browserNames() + ")")
	}
	if cmd.Flags().Changed("browser-profile") {
		if verr := validateBrowserProfile(profileFlag); verr != nil {
			return "", "", output.UsageError(verr.Error())
		}
	}
	return browserFlag, profileFlag, nil
}

// scheduleTarget resolves the browser profile the agents capture — the
// flags, capture.browser, or auto-selection — and the kinds selected.
// Resolution happens here, once: the agents are pinned to the browser
// and profile folder found, so a later change of OS default browser or
// last-used profile does not move them.
func scheduleTarget(cmd *cobra.Command) (chromium.Profile, []scheduleKind, error) {
	kinds, err := scheduleKindsFor(cmd)
	if err != nil {
		return chromium.Profile{}, nil, err
	}
	browserFlag, profileFlag, err := scheduleTargetFlags(cmd)
	if err != nil {
		return chromium.Profile{}, nil, err
	}
	p, err := resolveCaptureTarget(cmd, browserFlag, profileFlag)
	if err != nil {
		return chromium.Profile{}, nil, err
	}
	if verr := validateBrowserProfile(p.DirName); verr != nil {
		return chromium.Profile{}, nil, output.UsageError(verr.Error())
	}
	return p, kinds, nil
}

// changedGlobal returns a global flag's value only when the caller set
// it on the command line; env and config fallbacks are not baked into
// the agent.
func changedGlobal(cmd *cobra.Command, name string) string {
	f := cmd.Flags().Lookup(name)
	if f == nil || !f.Changed {
		return ""
	}
	return f.Value.String()
}

func scheduleInterval(cmd *cobra.Command, flag string, kindOff bool) (time.Duration, error) {
	d, _ := cmd.Flags().GetDuration(flag)
	if kindOff && cmd.Flags().Changed(flag) {
		return 0, output.UsageError(fmt.Sprintf("--%s has no effect when that kind is left out", flag))
	}
	if d < minScheduleEvery {
		return 0, output.UsageError(fmt.Sprintf("--%s %s is below the %s minimum", flag, d, minScheduleEvery))
	}
	if d%time.Second != 0 {
		return 0, output.UsageError(fmt.Sprintf("--%s %s must be whole seconds", flag, d))
	}
	return d, nil
}

func runCaptureScheduleInstall(cmd *cobra.Command, _ []string) error {
	env, err := scheduleEnvForRun(cmd)
	if err != nil {
		return err
	}
	target, kinds, err := scheduleTarget(cmd)
	if err != nil {
		return err
	}
	noTabs, _ := cmd.Flags().GetBool("no-tabs")
	noHistory, _ := cmd.Flags().GetBool("no-history")
	historyEvery, err := scheduleInterval(cmd, "history-every", noHistory)
	if err != nil {
		return err
	}
	tabsEvery, err := scheduleInterval(cmd, "tabs-every", noTabs)
	if err != nil {
		return err
	}

	type planned struct {
		agent scheduleAgent
		plist string
	}
	plan := make([]planned, 0, len(kinds))
	for _, k := range kinds {
		a := scheduleAgent{
			Kind:           k,
			Browser:        string(target.Browser),
			BrowserProfile: target.DirName,
			Instance:       changedGlobal(cmd, "instance"),
			FocusProfile:   changedGlobal(cmd, "profile"),
			Binary:         env.binary,
			LogDir:         env.logDir,
			Home:           env.home,
			PathEnv:        env.pathEnv,
			Interval:       historyEvery,
		}
		if k == kindTabs {
			a.Interval = tabsEvery
		}
		body, err := renderSchedulePlist(a)
		if err != nil {
			return err
		}
		plan = append(plan, planned{agent: a, plist: body})
	}

	w := cmd.OutOrStdout()
	if kitcli.IsDryRun(cmd) {
		if cliformat.Structured() {
			var actions []cliformat.Action
			for _, p := range plan {
				path := env.plistPath(p.agent.label())
				actions = append(actions,
					cliformat.Action{Kind: "write", Target: "file:" + path, Detail: strings.Join(p.agent.ProgramArguments()[1:], " "), Count: 1, Reversible: true},
					cliformat.Action{Kind: "load", Target: "launchd:" + p.agent.label(), Detail: "every " + p.agent.Interval.String(), Count: 1, Reversible: true})
			}
			return cliformat.EncodePreview(w, cliformat.NewPlan(cmd.CommandPath(), actions))
		}
		for _, p := range plan {
			fmt.Fprintf(w, "# would write %s and load it with launchctl\n%s\n", env.plistPath(p.agent.label()), p.plist)
		}
		return nil
	}

	for _, dir := range []string{env.logDir, env.agentsDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	rows := make([]scheduleRow, 0, len(plan))
	for _, p := range plan {
		label := p.agent.label()
		path := env.plistPath(label)
		// Unload first: launchd refuses to bootstrap a label it
		// already has, and the old job must not keep the old interval.
		env.launchctl.Bootout(label, path)
		if err := os.WriteFile(path, []byte(p.plist), 0o644); err != nil { //nolint:gosec // launchd requires a world-readable agent plist
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := env.launchctl.Bootstrap(path); err != nil {
			// launchd loads every plist here at next login; a job
			// that failed to load now must not start later unasked.
			_ = os.Remove(path)
			return fmt.Errorf("load %s: %w", label, err)
		}
		rows = append(rows, newScheduleRow(env, p.agent))
	}

	if cliformat.Structured() {
		return cliformat.Encode(w, rows)
	}
	for _, r := range rows {
		writeScheduleSummary(w, r)
	}
	return nil
}

func writeScheduleSummary(w io.Writer, r scheduleRow) {
	fmt.Fprintf(w, "installed %s\n", r.Label)
	fmt.Fprintf(w, "  runs:  ctxt capture %s every %s\n", r.Kind, r.Every)
	fmt.Fprintf(w, "  plist: %s\n", r.Plist)
	fmt.Fprintf(w, "  logs:  %s\n         %s\n", r.StdoutLog, r.StderrLog)
}

func runCaptureScheduleUninstall(cmd *cobra.Command, _ []string) error {
	env, err := scheduleEnvForRun(cmd)
	if err != nil {
		return err
	}
	browser, profile, kinds, err := scheduleUninstallTarget(cmd, env)
	if err != nil {
		return err
	}

	type target struct{ label, path string }
	var targets []target
	for _, k := range kinds {
		label := scheduleLabel(k, browser, profile)
		path := env.plistPath(label)
		if env.installed(label) {
			targets = append(targets, target{label: label, path: path})
		}
	}

	w := cmd.OutOrStdout()
	if kitcli.IsDryRun(cmd) {
		if cliformat.Structured() {
			var actions []cliformat.Action
			for _, t := range targets {
				actions = append(actions,
					cliformat.Action{Kind: "unload", Target: "launchd:" + t.label, Count: 1, Reversible: true},
					cliformat.Action{Kind: "remove", Target: "file:" + t.path, Count: 1, Reversible: true})
			}
			return cliformat.EncodePreview(w, cliformat.NewPlan(cmd.CommandPath(), actions))
		}
		for _, t := range targets {
			fmt.Fprintf(w, "would unload %s and remove %s\n", t.label, t.path)
		}
		if len(targets) == 0 {
			fmt.Fprintf(w, "nothing to uninstall for %s profile %q\n", browser, profile)
		}
		return nil
	}

	res := scheduleRemoval{Removed: []string{}}
	for _, t := range targets {
		env.launchctl.Bootout(t.label, t.path)
		if err := os.Remove(t.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", t.path, err)
		}
		res.Removed = append(res.Removed, t.label)
	}
	if cliformat.Structured() {
		return cliformat.Encode(w, res)
	}
	for _, l := range res.Removed {
		fmt.Fprintf(w, "uninstalled %s\n", l)
	}
	if len(res.Removed) == 0 {
		fmt.Fprintf(w, "nothing to uninstall for %s profile %q\n", browser, profile)
	}
	return nil
}

// scheduleUninstallTarget resolves the agents uninstall removes. It
// resolves like install, so the same flags reach the same agents. When
// the browser or profile is gone from disk but --browser and
// --browser-profile name agents that are still installed (by the
// profile folder pinned in them), those agents are the target: a
// deleted profile must stay uninstallable.
func scheduleUninstallTarget(cmd *cobra.Command, env *scheduleEnv) (browser, profile string, kinds []scheduleKind, err error) {
	target, kinds, err := scheduleTarget(cmd)
	if err == nil {
		return string(target.Browser), target.DirName, kinds, nil
	}
	kinds, kerr := scheduleKindsFor(cmd)
	browserFlag, _ := cmd.Flags().GetString("browser")
	profileFlag, _ := cmd.Flags().GetString("browser-profile")
	b, perr := chromium.ParseBrowser(browserFlag)
	if kerr != nil || perr != nil || validateBrowserProfile(profileFlag) != nil {
		return "", "", nil, err
	}
	for _, k := range kinds {
		if env.installed(scheduleLabel(k, string(b), profileFlag)) {
			return string(b), profileFlag, kinds, nil
		}
	}
	return "", "", nil, err
}

// scheduleRemoval is uninstall's machine-readable result.
type scheduleRemoval struct {
	Removed []string `json:"removed"`
}

// installed reports whether label has a plist or is loaded.
func (e *scheduleEnv) installed(label string) bool {
	if _, err := os.Stat(e.plistPath(label)); err == nil {
		return true
	}
	return e.launchctl.Loaded(label)
}

func runCaptureScheduleList(cmd *cobra.Command, _ []string) error {
	env, err := scheduleEnvForRun(cmd)
	if err != nil {
		return err
	}
	matches, err := filepath.Glob(filepath.Join(env.agentsDir, scheduleLabelPrefix+"*.plist"))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	rows := make([]scheduleRow, 0, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		a, err := parseSchedulePlist(data)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		a.LogDir = env.logDir
		row := newScheduleRow(env, a)
		row.Plist = path
		rows = append(rows, row)
	}
	// Machine formats always get a document, [] included; kit's row
	// dispatch writes nothing at all for an empty json result.
	if cliformat.Structured() {
		return cliformat.Encode(cmd.OutOrStdout(), rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), "no browser capture agents installed")
		return nil
	}
	return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), rows)
}
