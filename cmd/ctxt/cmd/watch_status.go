package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/watcher/watchstate"
)

// watchStatusReport is `ctxt watch status --format json`.
type watchStatusReport struct {
	Instance  string          `json:"instance"`
	Endpoint  string          `json:"endpoint"`
	Running   bool            `json:"running"`
	PID       int             `json:"pid,omitempty"`
	StartedAt *time.Time      `json:"started_at,omitempty"`
	Clipboard watchClipStatus `json:"clipboard"`
	StateFile string          `json:"state_file"`
	Dirs      []watchDirRow   `json:"dirs"`
}

type watchClipStatus struct {
	Status       string        `json:"status"`
	PollInterval time.Duration `json:"poll_interval"`
	MinLength    int           `json:"min_length"`
}

type watchDirRow struct {
	Path string `json:"path"`
	// Configured: the directory is in watch.dirs.
	Configured bool `json:"configured"`
	// Watching: the running watcher watches it.
	Watching      bool       `json:"watching"`
	Files         int        `json:"files"`
	LastEnqueueAt *time.Time `json:"last_enqueue_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
}

// runWatchStatus reports the local watcher from this machine's run and
// state files. It never calls dpkms.
func runWatchStatus(cmd *cobra.Command, _ []string) error {
	r, err := resolveEndpoint(cmd)
	if err != nil {
		return err
	}
	stateDir, err := watchstate.Dir()
	if err != nil {
		return err
	}
	info, alive, err := watchstate.ReadRun(watchstate.RunPath(stateDir, r.Key))
	if err != nil {
		return err
	}
	statePath := watchstate.StatePath(stateDir, r.Key)
	snap, err := watchstate.Read(statePath)
	if err != nil {
		return err
	}

	clipStatus, _ := clipboardState()
	rep := watchStatusReport{
		Instance: r.Key, Endpoint: config.RedactDSN(r.URL), Running: alive, StateFile: statePath,
		Clipboard: watchClipStatus{
			Status:       clipStatus,
			PollInterval: cfg.Watch.Clipboard.PollInterval,
			MinLength:    cfg.Watch.Clipboard.MinLength,
		},
	}
	if alive {
		rep.PID, rep.StartedAt = info.PID, timePtr(info.StartedAt)
	}

	rows := map[string]*watchDirRow{}
	row := func(p string) *watchDirRow {
		if rows[p] == nil {
			rows[p] = &watchDirRow{Path: p}
		}
		return rows[p]
	}
	for _, raw := range cfg.Watch.Dirs {
		if p, err := absWatchDir(raw); err == nil {
			row(p).Configured = true
		}
	}
	if alive {
		for _, p := range info.Dirs {
			row(p).Watching = true
		}
	}
	for p, d := range snap.Dirs {
		rw := row(p)
		rw.Files = len(d.Files)
		rw.LastEnqueueAt, rw.LastError, rw.LastErrorAt = timePtr(d.LastEnqueueAt), d.LastError, timePtr(d.LastErrorAt)
	}
	for _, p := range sortedKeys(rows) {
		rep.Dirs = append(rep.Dirs, *rows[p])
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), rep)
	}
	printWatchStatus(cmd, rep)
	return nil
}

func sortedKeys(m map[string]*watchDirRow) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func printWatchStatus(cmd *cobra.Command, rep watchStatusReport) {
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Instance          : %s (%s)\n", rep.Instance, rep.Endpoint)
	if rep.Running {
		since := ""
		if rep.StartedAt != nil {
			since = ", since " + rep.StartedAt.Local().Format(time.DateTime)
		}
		fmt.Fprintf(w, "Watcher           : running (pid %d%s)\n", rep.PID, since)
	} else {
		fmt.Fprintln(w, "Watcher           : not running")
	}
	fmt.Fprintf(w, "Clipboard watcher : %s\n", rep.Clipboard.Status)
	fmt.Fprintf(w, "State file        : %s\n", rep.StateFile)
	fmt.Fprintln(w)

	if len(rep.Dirs) == 0 {
		fmt.Fprintln(w, "No directories configured (watch.dirs).")
		return
	}
	stamp := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.Local().Format(time.DateTime)
	}
	yes := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	headers := []string{"Path", "Configured", "Watching", "Files", "Last Enqueue", "Last Error"}
	rows := make([][]string, 0, len(rep.Dirs))
	for _, d := range rep.Dirs {
		lastErr := d.LastError
		if lastErr != "" {
			lastErr = stamp(d.LastErrorAt) + " " + lastErr
		}
		rows = append(rows, []string{d.Path, yes(d.Configured), yes(d.Watching), strconv.Itoa(d.Files), stamp(d.LastEnqueueAt), lastErr})
	}
	printTable(w, headers, rows)
}
