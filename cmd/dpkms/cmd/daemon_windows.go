//go:build windows

package cmd

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// detachedProcAttr starts the daemon without a console and in its own
// process group, so closing the launching console neither ends it nor
// sends it console control events.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}
