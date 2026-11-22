//go:build windows

package podrun

import (
	"context"
)

// Run refuses pod execution on Windows until a job-object process-tree killer
// is implemented (F19).
func (r *ExecRunner) Run(_ context.Context, _ []string, _ RunOpts) (Result, error) {
	return Result{}, windowsUnsupportedError("daemon.runner")
}

// Start refuses pod execution on Windows until a job-object process-tree
// killer is implemented (F19).
func (r *ExecRunner) Start(_ context.Context, _ []string, _ RunOpts) (Proc, error) {
	return nil, windowsUnsupportedError("daemon.runner")
}
