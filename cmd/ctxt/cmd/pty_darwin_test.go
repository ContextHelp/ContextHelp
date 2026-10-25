package cmd

import (
	"bytes"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY returns the master and slave ends of a new pseudo-terminal.
func openPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	m := os.NewFile(uintptr(fd), "/dev/ptmx")
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		m.Close()
		return nil, nil, err
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		m.Close()
		return nil, nil, err
	}
	buf := make([]byte, 128)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&buf[0]))); errno != 0 { //nolint:staticcheck // x/sys has no wrapper for TIOCPTYGNAME; test-only pty helper
		m.Close()
		return nil, nil, errno
	}
	name := string(buf[:bytes.IndexByte(buf, 0)])
	s, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
