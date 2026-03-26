package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
	"github.com/spf13/cobra"
)

// watcherCmd is the top-level "watcher" command group.
var watcherCmd = &cobra.Command{
	Use:   "watcher",
	Short: "Manage passive background watchers (clipboard, directories)",
	Long: `Start, stop, inspect, and configure passive background watchers.

Watchers monitor the clipboard and filesystem directories, silently enqueueing
ingestion jobs when new or changed content is detected.

Configuration (config.yaml):
  watch:
    clipboard:
      enabled: false       # must opt-in explicitly
      poll_interval: 2s
      min_length: 80
      auto_ingest: true

  CTXT_NO_CLIPBOARD=1 disables the clipboard watcher unconditionally.

Examples:
  ctxt watcher enable clipboard
  ctxt watcher start
  ctxt watcher status
  ctxt watcher stop`,
}

var watcherStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all configured watchers and block",
	Long: `Start clipboard and directory watchers as configured in config.yaml.

Blocks until interrupted (Ctrl-C or SIGTERM).`,
	RunE: runWatcherStart,
}

var watcherStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all active directory watchers",
	Long:  `Mark all active directory watch configs as paused in storage.`,
	RunE:  runWatcherStop,
}

var watcherStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all configured watchers",
	Long:  `Display clipboard watcher status and all directory watch configurations.`,
	RunE:  runWatcherStatus,
}

var watcherEnableCmd = &cobra.Command{
	Use:   "enable <watcher>",
	Short: "Enable a watcher by name (e.g. clipboard)",
	Long: `Enable a named watcher. Supported names: clipboard.

This writes the change to the active config. Restart 'ctxt watcher start'
to apply.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatcherEnable,
}

var watcherDisableCmd = &cobra.Command{
	Use:   "disable <watcher>",
	Short: "Disable a watcher by name (e.g. clipboard)",
	Long: `Disable a named watcher. Supported names: clipboard.

This writes the change to the active config. The running watcher will stop
at the next poll cycle if already running.`,
	Args: cobra.ExactArgs(1),
	RunE: runWatcherDisable,
}

func init() {
	rootCmd.AddCommand(watcherCmd)
	watcherCmd.AddCommand(watcherStartCmd)
	watcherCmd.AddCommand(watcherStopCmd)
	watcherCmd.AddCommand(watcherStatusCmd)
	watcherCmd.AddCommand(watcherEnableCmd)
	watcherCmd.AddCommand(watcherDisableCmd)
}

func runWatcherStart(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Fprintln(cmd.OutOrStdout(), "\nStopping watchers...")
		cancel()
	}()

	mgr := watcher.NewManager(svc.Store.Watches(), svc)

	// Start clipboard watcher if enabled.
	clipCfg := watcher.ClipboardConfig{
		Enabled:      cfg.Watch.Clipboard.Enabled,
		PollInterval: cfg.Watch.Clipboard.PollInterval,
		MinLength:    cfg.Watch.Clipboard.MinLength,
		AutoIngest:   cfg.Watch.Clipboard.AutoIngest,
	}
	if clipCfg.PollInterval == 0 {
		clipCfg.PollInterval = 2 * time.Second
	}
	if clipCfg.MinLength == 0 {
		clipCfg.MinLength = 80
	}

	clipWatcher := watcher.NewClipboardWatcher(clipCfg, svc, nil)

	noClip := os.Getenv("CTXT_NO_CLIPBOARD") == "1"
	if cfg.Watch.Clipboard.Enabled && !noClip {
		fmt.Fprintln(cmd.OutOrStdout(), "Clipboard watcher: enabled")
		go clipWatcher.Run(ctx)
	} else if noClip {
		fmt.Fprintln(cmd.OutOrStdout(), "Clipboard watcher: disabled (CTXT_NO_CLIPBOARD=1)")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(),
			"Clipboard watcher: disabled (use 'ctxt watcher enable clipboard' to activate)")
	}

	// Directory watchers.
	dirs := cfg.Watch.Dirs
	if len(dirs) == 0 {
		dirs = cfg.Watch.Paths
	}
	for _, dir := range dirs {
		mode := watcher.DetectMode(dir)
		wcfg := &storage.WatchConfig{
			ID:              "dir:" + dir,
			Path:            dir,
			Mode:            mode,
			IncludePatterns: cfg.Watch.Patterns,
			DebounceMS:      int(cfg.Watch.Debounce.Milliseconds()),
			Status:          "active",
			CreatedAt:       time.Now(),
			UpdatedAt:       time.Now(),
		}
		if wcfg.DebounceMS <= 0 {
			wcfg.DebounceMS = 500
		}
		if err := mgr.AddWatch(ctx, wcfg); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to start watch for %s: %v\n", dir, err)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Directory watcher: %s (mode=%s)\n", dir, mode)
		}
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Watchers running. Press Ctrl-C to stop.")
	<-ctx.Done()
	mgr.Stop()
	return nil
}

func runWatcherStop(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	watches, err := svc.Store.Watches().ListWatches(ctx, "active")
	if err != nil {
		return fmt.Errorf("list watches: %w", err)
	}

	for _, w := range watches {
		w.Status = "paused"
		w.UpdatedAt = time.Now()
		if err := svc.Store.Watches().UpdateWatch(ctx, w); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to pause watch %s: %v\n", w.ID, err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Stopped %d watch(es)\n", len(watches))
	return nil
}

func runWatcherStatus(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	active, err := svc.Store.Watches().ListWatches(ctx, "active")
	if err != nil {
		return fmt.Errorf("list active watches: %w", err)
	}
	paused, err := svc.Store.Watches().ListWatches(ctx, "paused")
	if err != nil {
		return fmt.Errorf("list paused watches: %w", err)
	}

	all := append(active, paused...)

	clipStatus := "disabled"
	if cfg.Watch.Clipboard.Enabled {
		clipStatus = "enabled"
	}
	if os.Getenv("CTXT_NO_CLIPBOARD") == "1" {
		clipStatus = "disabled (CTXT_NO_CLIPBOARD)"
	}

	if isJSONOutput() {
		type clipInfo struct {
			Status       string        `json:"status"`
			PollInterval time.Duration `json:"poll_interval"`
			MinLength    int           `json:"min_length"`
		}
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"clipboard": clipInfo{
				Status:       clipStatus,
				PollInterval: cfg.Watch.Clipboard.PollInterval,
				MinLength:    cfg.Watch.Clipboard.MinLength,
			},
			"directory_watches": all,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Clipboard watcher : %s\n", clipStatus)
	fmt.Fprintln(cmd.OutOrStdout())

	if len(all) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No directory watches configured.")
		return nil
	}

	headers := []string{"ID", "Path", "Mode", "Status", "Last Error"}
	rows := make([][]string, 0, len(all))
	for _, w := range all {
		rows = append(rows, []string{w.ID, w.Path, w.Mode, w.Status, w.LastError})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

// runWatcherEnable handles: ctxt watcher enable <name>
func runWatcherEnable(cmd *cobra.Command, args []string) error {
	name := args[0]
	switch name {
	case "clipboard":
		return setClipboardEnabled(cmd, true)
	default:
		return fmt.Errorf("unknown watcher %q (supported: clipboard)", name)
	}
}

// runWatcherDisable handles: ctxt watcher disable <name>
func runWatcherDisable(cmd *cobra.Command, args []string) error {
	name := args[0]
	switch name {
	case "clipboard":
		return setClipboardEnabled(cmd, false)
	default:
		return fmt.Errorf("unknown watcher %q (supported: clipboard)", name)
	}
}

// setClipboardEnabled writes the enabled flag to config.
func setClipboardEnabled(cmd *cobra.Command, enabled bool) error {
	cfg.Watch.Clipboard.Enabled = enabled
	if err := writeWatcherConfig(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Clipboard watcher %s.\n", state)
	if enabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'ctxt watcher start' to begin monitoring.")
	}
	return nil
}

// writeWatcherConfig persists the current cfg back to the config file.
func writeWatcherConfig() error {
	return config.WriteBack(cfg, configPath())
}
