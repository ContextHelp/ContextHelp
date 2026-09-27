//go:build unix

package launch

import (
	"os/exec"
	"syscall"
)

// setGroup starts cmd as the leader of a new process group, so killGroup
// reaches the helpers Chrome spawns.
func setGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killGroup SIGKILLs the process group led by cmd.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
