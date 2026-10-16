//go:build unix

package cmd

import "syscall"

// detachedProcAttr starts the daemon in a new session, detached from the
// launching terminal.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
