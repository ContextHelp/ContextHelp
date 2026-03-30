//go:build darwin || linux

package steps

import "syscall"

func (se *StepExecutor) setMemoryLimit(limit string) error {
	rlim := &syscall.Rlimit{}
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, rlim); err != nil {
		return err
	}
	rlim.Cur = parseMemoryLimit(limit)
	rlim.Max = rlim.Cur
	return syscall.Setrlimit(syscall.RLIMIT_AS, rlim)
}

func (se *StepExecutor) setCPULimit(limit string) error {
	rlim := &syscall.Rlimit{}
	if err := syscall.Getrlimit(syscall.RLIMIT_CPU, rlim); err != nil {
		return err
	}
	rlim.Cur = parseCPULimit(limit)
	rlim.Max = rlim.Cur
	return syscall.Setrlimit(syscall.RLIMIT_CPU, rlim)
}
