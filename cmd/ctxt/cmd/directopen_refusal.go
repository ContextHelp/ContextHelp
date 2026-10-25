package cmd

import (
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/spf13/cobra"
)

// daemonHeldError is a ctxt write refused because the daemon holds the
// database lock. The command writes the store directly, so it cannot run
// beside dpkms serve: its fix names the exact commands to stop the
// daemon and start it again. It wraps the *directopen.ContentionError
// (and so dblock.ErrHeld).
type daemonHeldError struct {
	err     *directopen.ContentionError
	command string
	port    int
}

func (e *daemonHeldError) Error() string { return e.err.Error() }
func (e *daemonHeldError) Unwrap() error { return e.err }

// Fix states the stop, rerun, restart sequence.
func (e *daemonHeldError) Fix() string {
	stop := "`dpkms shutdown`"
	if e.port > 0 {
		stop = fmt.Sprintf("`dpkms shutdown --port %d`", e.port)
	}
	return fmt.Sprintf("`%s` writes the database directly and cannot run while dpkms serve holds it: "+
		"stop the daemon with %s, rerun the command, then start the daemon again with `dpkms serve`",
		e.command, stop)
}

// explainDaemonHeld turns a daemon-held lock refusal for cmd on dbPath
// into a *daemonHeldError; any other error is returned unchanged.
func explainDaemonHeld(err error, cmd *cobra.Command, dbPath string) error {
	var ce *directopen.ContentionError
	if !errors.As(err, &ce) || ce.Held == nil || ce.Held.Holder == nil || ce.Held.Holder.Role != dblock.RoleDaemon {
		return err
	}
	command := "ctxt"
	if cmd != nil {
		command = cmd.CommandPath()
	}
	return &daemonHeldError{err: ce, command: command, port: daemonPort(dbPath)}
}

// daemonPort returns the port of the running local instance serving
// dbPath, from its pidfile; 0 when none is found.
func daemonPort(dbPath string) int {
	runDir, err := config.RunDirPath(dataDir())
	if err != nil {
		return 0
	}
	infos, err := pidfile.Scan(runDir)
	if err != nil {
		return 0
	}
	want := dblock.Canonical(dbPath)
	for _, info := range infos {
		if info.DBPath != "" && dblock.Canonical(info.DBPath) == want {
			return info.Port
		}
	}
	return 0
}
