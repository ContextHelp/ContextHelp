package pidfile

import (
	"errors"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// startTimeTolerance absorbs the OS clock's resolution when comparing a
// process's start time with the started_at it recorded. Linux derives
// start times from the boot time in whole seconds, so a process can be
// reported up to about a second early; macOS reports milliseconds. Late
// readings only come from wall-clock steps, so the margin errs towards
// keeping an entry live.
const startTimeTolerance = 5 * time.Second

// errNotRunning reports that the pid exited between the liveness probe
// and the start-time read.
var errNotRunning = process.ErrorProcessNotRunning

// processStartTime returns the OS-reported start time of pid. It is a
// variable so tests can simulate a start time the OS will not report.
var processStartTime = func(pid int) (time.Time, error) {
	p, err := process.NewProcess(int32(pid)) // #nosec G115 -- pids fit in int32 on every supported OS
	if err != nil {
		return time.Time{}, err
	}
	ms, err := p.CreateTime()
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}

// sameProcess reports whether the running process with info's pid can be
// the one that wrote info. A process that started after the entry was
// written got the pid by reuse. An entry without started_at, or a pid
// whose start time the OS will not report, cannot be checked and counts
// as the same process.
func sameProcess(info Info) bool {
	if info.StartedAt.IsZero() {
		return true
	}
	started, err := processStartTime(info.PID)
	if err != nil {
		return !errors.Is(err, errNotRunning)
	}
	return !started.After(info.StartedAt.Add(startTimeTolerance))
}

// alive reports whether a process with the given pid is running. A pid
// this user may not signal (EPERM) is running too: the run dir may be
// shared with another user's instance (see the package doc). On Windows,
// where signal 0 is not supported, the process is opened and asked
// whether it has exited.
func alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	ok, err := process.PidExists(int32(pid)) // #nosec G115 -- pids fit in int32 on every supported OS
	return err == nil && ok
}
