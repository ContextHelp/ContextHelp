package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/watcher"
	"github.com/spf13/cobra"
)

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Manage background watchers (clipboard, directories)",
	Long: `Start, stop, and inspect background watchers.

Watchers monitor the clipboard and filesystem directories, automatically
enqueueing ingestion jobs when new or changed content is detected.

Examples:
  # Start all configured watchers and block until Ctrl-C
  ctxt watch start

  # Stop all active watchers (persisted in storage)
  ctxt watch stop

  # Show status of all active watches
  ctxt watch status`,
}

var watchStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start all configured watchers",
	Long: `Start clipboard and directory watchers as configured in config.yaml.

Blocks until interrupted (Ctrl-C or SIGTERM). Clipboard watcher respects
watch.clipboard.enabled and CTXT_NO_CLIPBOARD env var. Directory watchers
are loaded from storage (persisted watch configs).`,
	RunE: runWatchStart,
}

var watchStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop all active directory watchers",
	Long:  `Mark all active directory watch configs as paused in storage.`,
	RunE:  runWatchStop,
}

var watchStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of all active watches",
	Long:  `List all watch configurations stored in the database with their status.`,
	RunE:  runWatchStatus,
}

func init() {
	rootCmd.AddCommand(watchCmd)
	watchCmd.AddCommand(watchStartCmd)
	watchCmd.AddCommand(watchStopCmd)
	watchCmd.AddCommand(watchStatusCmd)
}

func runWatchStart(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl-C / SIGTERM.
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

	if cfg.Watch.Clipboard.Enabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Clipboard watcher: enabled")
		go clipWatcher.Run(ctx)
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Clipboard watcher: disabled (set watch.clipboard.enabled: true to activate)")
	}

	// Directory watchers from dirs config.
	dirs := cfg.Watch.Dirs
	if len(dirs) == 0 {
		dirs = cfg.Watch.Paths // backwards compat
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

func runWatchStop(cmd *cobra.Command, _ []string) error {
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

func runWatchStatus(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	// List all (active + paused).
	active, err := svc.Store.Watches().ListWatches(ctx, "active")
	if err != nil {
		return fmt.Errorf("list active watches: %w", err)
	}
	paused, err := svc.Store.Watches().ListWatches(ctx, "paused")
	if err != nil {
		return fmt.Errorf("list paused watches: %w", err)
	}

	all := append(active, paused...)

	// Clipboard status from config.
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

	fmt.Fprintf(cmd.OutOrStdout(), "Clipboard watcher: %s\n", clipStatus)
	fmt.Fprintln(cmd.OutOrStdout())

	headers := []string{"ID", "Path", "Mode", "Status", "Last Error"}
	rows := make([][]string, 0, len(all))
	for _, w := range all {
		rows = append(rows, []string{
			w.ID, w.Path, w.Mode, w.Status, w.LastError,
		})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}
