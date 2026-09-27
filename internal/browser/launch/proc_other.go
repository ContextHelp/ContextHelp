//go:build !unix

package launch

import (
	"context"
	"os/exec"
	"runtime"
	"strconv"
)

// setGroup is a no-op: without process groups, killGroup walks the tree.
func setGroup(*exec.Cmd) {}

// killGroup kills cmd and, on Windows, every process it spawned.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == goosWindows {
		pid := strconv.Itoa(cmd.Process.Pid)
		if err := exec.CommandContext(context.Background(), "taskkill", "/T", "/F", "/PID", pid).Run(); err == nil { //nolint:gosec // fixed tool; pid is ours
			return nil
		}
	}
	return cmd.Process.Kill()
}
