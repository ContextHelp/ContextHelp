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
	pid     int
}

func (e *daemonHeldError) Error() string { return e.err.Error() }
func (e *daemonHeldError) Unwrap() error { return e.err }

// Fix states the stop, rerun, restart sequence.
func (e *daemonHeldError) Fix() string {
	var stop string
	switch {
	case e.port > 0:
		stop = fmt.Sprintf("`dpkms shutdown --port %d`", e.port)
	case e.pid > 0:
		stop = fmt.Sprintf("`dpkms shutdown --port <port>` (the daemon is pid %d; `dpkms ps` lists its port)", e.pid)
	default:
		stop = "`dpkms shutdown --port <port>` (`dpkms ps` lists the daemon's port)"
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
	pid := ce.Held.Holder.PID
	return &daemonHeldError{err: ce, command: command, port: daemonPort(dbPath, pid), pid: pid}
}

// daemonPort returns the port of the running local instance serving
// dbPath, from its pidfile, else from the pidfile of the lock holder's
// pid; 0 when neither is found.
func daemonPort(dbPath string, holderPID int) int {
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
	for _, info := range infos {
		if holderPID > 0 && info.PID == holderPID {
			return info.Port
		}
	}
	return 0
}
