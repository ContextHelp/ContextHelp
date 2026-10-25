package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
	"github.com/ideacrafterslabs/ctxt/internal/watcher/watchstate"
)

// watchCmd is the top-level "watcher" command group.
var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Manage passive background watchers (clipboard, directories)",
	Long: `Start, stop, inspect, and configure passive background watchers.

Watchers run on this machine. They monitor the clipboard and the
directories in watch.dirs and enqueue new or changed content on the one
dpkms instance ctxt resolves (--server, --instance, CTXT_INSTANCE, the
current instance, then server.urls / server.url), over its API.

What was already sent is ctxt's own record, one state file per instance
under $XDG_STATE_HOME/ctxt/watch/: switching instances sends each file
to the new one once. dpkms's server-side watches (/api/v1/watches) are
separate and never see these records.

Configuration (ctxt.yaml; 'ctxt config path' shows which file):
  watch:
    dirs: [~/notes]
    patterns: ["**/*.md"]
    debounce: 500ms
    clipboard:
      enabled: false       # must opt-in explicitly
      poll_interval: 2s
      min_length: 80
      auto_ingest: true

  CTXT_NO_CLIPBOARD=1 disables the clipboard watcher unconditionally.

Examples:
  ctxt watch enable clipboard
  ctxt watch start
  ctxt watch status
  ctxt watch stop`,
}

var watchStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all configured watchers and block",
	Long: `Start the clipboard and directory watchers configured in ctxt.yaml,
sending to the resolved dpkms instance, and block until interrupted
(Ctrl-C, SIGTERM, or 'ctxt watch stop').

On start each directory is scanned once: files that changed since the
last run against this instance are sent, unchanged ones are not. A
deleted file deletes the object it produced (that needs a token with
delete:objects, i.e. an admin token on protected instances).

Exits 70 when nothing answers at the instance and 5 when it rejects the
token, before watching anything; 4 when a watcher for the same instance
is already running; 2 when there is nothing to watch.`,
	RunE: runWatchStart,
}

var watchStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running watcher for the resolved instance",
	Long: `Signal the 'ctxt watch start' running for the resolved instance to stop,
and wait for it to exit. Succeeds when none is running.`,
	RunE: runWatchStop,
}

var watchStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the local watcher's state",
	Long: `Show whether a watcher runs for the resolved instance, the clipboard
setting, and per directory the files sent and the last error.

Reads this machine's run and state files only; it never calls dpkms.`,
	RunE: runWatchStatus,
}

var watchEnableCmd = &cobra.Command{
	Use:   "enable <watcher>",
	Short: "Enable a watcher by name (e.g. clipboard)",
	Long: `Enable a named watcher. Supported names: clipboard.

This writes the change to the active config. Restart 'ctxt watch start'
to apply.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchEnable,
}

var watchDisableCmd = &cobra.Command{
	Use:   "disable <watcher>",
	Short: "Disable a watcher by name (e.g. clipboard)",
	Long: `Disable a named watcher. Supported names: clipboard.

This writes the change to the active config. Restart 'ctxt watch start'
to apply.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatchDisable,
}

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.AddCommand(watchStartCmd)
	watchCmd.AddCommand(watchStopCmd)
	watchCmd.AddCommand(watchStatusCmd)
	watchCmd.AddCommand(watchEnableCmd)
	watchCmd.AddCommand(watchDisableCmd)
	for _, c := range []*cobra.Command{watchStartCmd, watchStopCmd, watchStatusCmd} {
		c.Flags().String("server", "", serverFlagUsage)
	}

	// start blocks running watchers; treat as a Write effect (it
	// enqueues ingestion jobs while running).
	cliconv.WithSideEffect(watchStartCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(watchStartCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(watchStartCmd, []cliconv.Example{
		{Title: "Start every configured watcher", Command: "ctxt watch start"},
		{Title: "Send to a named instance", Command: "ctxt watch start --instance home"},
		{Title: "Run without the clipboard watcher", Command: "CTXT_NO_CLIPBOARD=1 ctxt watch start"},
	})
	cliconv.WithNextSteps(watchStartCmd, []cliconv.NextStep{
		{When: "in another shell", Suggest: "ctxt watch status", Reason: "verify the watcher runs and files are sent"},
		{When: "to stop", Suggest: "ctxt watch stop", Reason: "stop the running watcher"},
	})
	cliconv.WithSideEffect(watchStopCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(watchStopCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(watchStopCmd, []cliconv.Example{
		{Title: "Stop the running watcher", Command: "ctxt watch stop"},
	})
	cliconv.WithNextSteps(watchStopCmd, []cliconv.NextStep{
		{When: "after stop", Suggest: "ctxt watch status", Reason: "confirm no watcher runs"},
	})
	cliconv.WithSideEffect(watchStatusCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(watchStatusCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(watchStatusCmd, []cliconv.Example{
		{Title: "Show watcher status", Command: "ctxt watch status"},
		{Title: "Emit JSON", Command: "ctxt watch status --format json"},
	})
	cliconv.WithSideEffect(watchEnableCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(watchEnableCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(watchEnableCmd, []cliconv.Example{
		{Title: "Enable the clipboard watcher", Command: "ctxt watch enable clipboard"},
	})
	cliconv.WithNextSteps(watchEnableCmd, []cliconv.NextStep{
		{When: "after enable", Suggest: "ctxt watch start", Reason: "start monitoring with the new config"},
	})
	cliconv.WithSideEffect(watchDisableCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(watchDisableCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(watchDisableCmd, []cliconv.Example{
		{Title: "Disable the clipboard watcher", Command: "ctxt watch disable clipboard"},
	})
	cliconv.WithNextSteps(watchDisableCmd, []cliconv.NextStep{
		{When: "after disable", Suggest: "ctxt watch status", Reason: "confirm the clipboard watcher is now disabled"},
	})
}

// watchScanHook, when set (tests), is told when a directory's initial
// scan has finished.
var watchScanHook func(watchID string)

// clipboardState describes the configured clipboard watcher.
func clipboardState() (status string, on bool) {
	switch {
	case os.Getenv("CTXT_NO_CLIPBOARD") == "1":
		return "disabled (CTXT_NO_CLIPBOARD=1)", false
	case cfg.Watch.Clipboard.Enabled:
		return "enabled", true
	default:
		return "disabled", false
	}
}

func runWatchStart(cmd *cobra.Command, _ []string) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()
	dirs, err := watchDirs()
	if err != nil {
		return err
	}
	clipStatus, clipOn := clipboardState()
	if len(dirs) == 0 && !clipOn {
		e := output.UsageError("nothing to watch: watch.dirs is empty and the clipboard watcher is " + clipStatus)
		e.SuggestedFix = "add directories to watch.dirs, or run `ctxt watch enable clipboard`"
		return e
	}

	r, err := resolveEndpoint(cmd)
	if err != nil {
		return err
	}
	client, err := dpkmsclient.New(r.Endpoint, clientOptions(cmd)...)
	if err != nil {
		return err
	}
	stateDir, err := watchstate.Dir()
	if err != nil {
		return err
	}

	run, err := watchstate.AcquireRun(watchstate.RunPath(stateDir, r.Key), watchstate.RunInfo{
		Instance: r.Key, Endpoint: client.URL(), Dirs: dirs, Clipboard: clipOn,
	})
	if err != nil {
		var running *watchstate.RunningError
		if errors.As(err, &running) {
			e := output.WrapError(err, output.CodeConflict, output.ExitConflict)
			e.SuggestedFix = "stop it with `ctxt watch stop`, or watch another instance with --instance"
			return e
		}
		return err
	}
	defer func() { _ = run.Release() }()

	store, err := watchstate.Open(watchstate.StatePath(stateDir, r.Key), r.Key)
	if err != nil {
		return err
	}
	defer func() {
		if err := store.Close(); err != nil {
			fmt.Fprintf(errOut, "Warning: save watch state: %v\n", err)
		}
	}()

	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Loud failure before watching anything: nothing answering is 70, a
	// rejected token 5 (ADR-077 §2). Later failures are per file: each
	// is recorded as its directory's last error and retried by the
	// file's next change or the next start.
	if err := client.Get(ctx, "/api/v1/whoami", nil, nil); err != nil {
		return err
	}

	ing := watchIngester{client}
	records := loggedRecords{FileRecords: store, w: errOut}
	fmt.Fprintf(out, "Watching for instance %s (%s)\n", r.Key, client.URL())
	fmt.Fprintf(out, "State file: %s\n", store.Path())

	mgr := watcher.NewManager(nil, records, ing, watcher.WithInitialScan(), watcher.WithScanDone(func(id string) {
		if watchScanHook != nil {
			watchScanHook(id)
		}
	}))
	defer mgr.Stop()

	fmt.Fprintf(out, "Clipboard watcher: %s\n", clipStatus)
	renderHints(cmd)
	if clipOn {
		clip := watcher.NewClipboardWatcher(watcher.ClipboardConfig{
			Enabled:      true,
			PollInterval: cfg.Watch.Clipboard.PollInterval,
			MinLength:    cfg.Watch.Clipboard.MinLength,
			AutoIngest:   cfg.Watch.Clipboard.AutoIngest,
			Dedup:        store,
			OnError:      func(err error) { fmt.Fprintf(errOut, "watch: %v\n", err) },
		}, ing, nil)
		go clip.Run(ctx)
	}

	debounce := int(cfg.Watch.Debounce.Milliseconds())
	if debounce <= 0 {
		debounce = 500
	}
	for _, dir := range dirs {
		mode := watcher.DetectMode(dir)
		_ = mgr.AddWatch(ctx, &watcher.WatchConfig{
			ID:              dir,
			Path:            dir,
			Mode:            mode,
			IncludePatterns: cfg.Watch.Patterns,
			DebounceMS:      debounce,
			Status:          "active",
		})
		fmt.Fprintf(out, "Directory watcher: %s (mode=%s)\n", dir, mode)
	}

	fmt.Fprintln(out, "Watchers running. Press Ctrl-C or run 'ctxt watch stop' to stop.")
	<-ctx.Done()
	fmt.Fprintln(out, "Stopping watchers...")
	return nil
}

// watchDirs returns watch.dirs as absolute, cleaned paths, with a
// leading ~ expanded. A directory that doesn't exist is a USAGE error.
func watchDirs() ([]string, error) {
	var dirs []string
	seen := map[string]bool{}
	for _, raw := range cfg.Watch.Dirs {
		dir, err := absWatchDir(raw)
		if err != nil {
			return nil, err
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			e := output.UsageError(fmt.Sprintf("watch.dirs entry %q is not a directory", raw))
			e.SuggestedFix = "create it, or remove it from watch.dirs"
			return nil, e
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs, nil
}

func absWatchDir(raw string) (string, error) {
	p := raw
	if p == "~" || len(p) > 1 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand %q: %w", raw, err)
		}
		p = home + p[1:]
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("watch.dirs entry %q: %w", raw, err)
	}
	return abs, nil
}

// watchStopTimeout bounds how long stop waits for the watcher to exit.
const watchStopTimeout = 15 * time.Second

func runWatchStop(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return err
	}
	stateDir, err := watchstate.Dir()
	if err != nil {
		return err
	}
	path := watchstate.RunPath(stateDir, r.Key)
	info, alive, err := watchstate.ReadRun(path)
	if err != nil {
		return err
	}
	if !alive {
		removed, err := watchstate.RemoveStale(path)
		if err != nil {
			return err
		}
		msg := fmt.Sprintf("No watcher running for instance %s.", r.Key)
		if removed {
			msg += " Removed a stale run file."
		}
		fmt.Fprintln(out, msg)
		return nil
	}
	if info.PID <= 0 {
		return fmt.Errorf("the watcher for instance %s is starting; its run file %s has no pid yet, retry", r.Key, path)
	}
	proc, err := os.FindProcess(info.PID)
	if err != nil {
		return fmt.Errorf("find watcher pid %d: %w", info.PID, err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal watcher pid %d: %w", info.PID, err)
	}
	deadline := time.Now().Add(watchStopTimeout)
	for time.Now().Before(deadline) {
		if _, alive, err := watchstate.ReadRun(path); err == nil && !alive {
			fmt.Fprintf(out, "Stopped the watcher for instance %s (pid %d).\n", r.Key, info.PID)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	e := output.WrapError(fmt.Errorf("the watcher for instance %s (pid %d) did not stop within %s", r.Key, info.PID, watchStopTimeout),
		output.CodeTransient, output.ExitTransient)
	e.SuggestedFix = fmt.Sprintf("check it with `ctxt watch status`; `kill %d` ends it", info.PID)
	return e
}

// runWatchEnable handles: ctxt watch enable <name>
func runWatchEnable(cmd *cobra.Command, args []string) error {
	name := args[0]
	switch name {
	case "clipboard":
		return setClipboardEnabled(cmd, true)
	default:
		return fmt.Errorf("unknown watcher %q (supported: clipboard)", name)
	}
}

// runWatchDisable handles: ctxt watch disable <name>
func runWatchDisable(cmd *cobra.Command, args []string) error {
	name := args[0]
	switch name {
	case "clipboard":
		return setClipboardEnabled(cmd, false)
	default:
		return fmt.Errorf("unknown watcher %q (supported: clipboard)", name)
	}
}

// setClipboardEnabled writes watch.clipboard.enabled to the write
// target's own layer.
func setClipboardEnabled(cmd *cobra.Command, enabled bool) error {
	err := editConfig(func(l *config.Layer) error {
		return l.Set(enabled, "watch", "clipboard", "enabled")
	})
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Clipboard watcher %s.\n", state)
	if enabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'ctxt watch start' to begin monitoring.")
	}
	return nil
}
