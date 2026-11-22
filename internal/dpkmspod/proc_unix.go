//go:build unix

package dpkmspod

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// stopForwardPID signals a previously recorded detached forward helper only if
// the PID is still alive and appears to be a `pod` process, preventing signals
// to a reused unrelated PID.
func stopForwardPID(pid int, startedAt time.Time) {
	if pid <= 0 {
		return
	}
	if !looksLikePodForward(pid, startedAt) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

func looksLikePodForward(pid int, _ time.Time) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output() // #nosec G204 -- pid is an integer formatted with strconv.Itoa
	if err != nil {
		return false
	}
	comm := strings.ToLower(strings.TrimSpace(string(out)))
	return strings.Contains(comm, "pod")
}
