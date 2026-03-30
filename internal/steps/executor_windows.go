//go:build windows

package steps

import "fmt"

// setMemoryLimit is a no-op on Windows — RLIMIT_AS is a POSIX concept
// not available in the Windows kernel. Resource limits for steps on Windows
// would require Job Objects, which is out of scope for the initial port.
func (se *StepExecutor) setMemoryLimit(limit string) error {
	return fmt.Errorf("steps: memory limits are not supported on Windows")
}

// setCPULimit is a no-op on Windows — RLIMIT_CPU is POSIX-only.
func (se *StepExecutor) setCPULimit(limit string) error {
	return fmt.Errorf("steps: CPU limits are not supported on Windows")
}
